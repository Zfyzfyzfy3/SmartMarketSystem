package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Zfyzfyzfy3/SmartMarketSystem/gateway/middleware"
)

// gatewayHandler implements http.Handler and dispatches requests to the
// appropriate downstream service based on prefix matching.
type gatewayHandler struct {
	routes    []route
	proxies   *proxyCache
	transport http.RoundTripper
}

// route couples an HTTP path prefix with a service entry.
type route struct {
	prefix  string
	service *serviceEntry
}

func newGatewayHandler(entries []*serviceEntry) *gatewayHandler {
	routes := make([]route, 0)
	for _, entry := range entries {
		prefixes := entry.listPrefixes()
		for _, prefix := range prefixes {
			routes = append(routes, route{prefix: prefix, service: entry})
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		return len(routes[i].prefix) > len(routes[j].prefix)
	})

	return &gatewayHandler{
		routes:    routes,
		proxies:   newProxyCache(),
		transport: defaultTransport(),
	}
}

func (h *gatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL == nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	path := r.URL.Path
	entry := h.match(path)
	if entry == nil {
		http.NotFound(w, r)
		return
	}

	target, err := entry.pickEndpoint()
	if err != nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	proxy := h.proxies.Get(target, h.transport)

	// Propagate tracing headers and request id downstream.
	if reqID := middleware.GetRequestID(r.Context()); reqID != "" {
		r.Header.Set("X-Request-ID", reqID)
		w.Header().Set("X-Request-ID", reqID)
	}
	if traceID := r.Header.Get("X-Trace-ID"); traceID != "" {
		w.Header().Set("X-Trace-ID", traceID)
	}
	w.Header().Set("X-Upstream-Service", entry.name)

	proxy.ServeHTTP(w, r)
}

// match executes longest-prefix matching to find the target service for
// the incoming request path. The routes slice is pre-sorted so the first
// match is the most specific.
func (h *gatewayHandler) match(path string) *serviceEntry {
	for _, route := range h.routes {
		if strings.HasPrefix(path, route.prefix) {
			return route.service
		}
	}
	return nil
}

// proxyCache stores reusable reverse proxy instances per upstream host.
type proxyCache struct {
	mu      sync.RWMutex
	proxies map[string]*httputil.ReverseProxy
	metrics *proxyMetrics
}

// proxyMetrics records the most recent proxy error to aid debugging.
type proxyMetrics struct {
	mu         sync.Mutex
	lastError  error
	lastTarget string
}

func newProxyCache() *proxyCache {
	return &proxyCache{
		proxies: make(map[string]*httputil.ReverseProxy),
		metrics: &proxyMetrics{},
	}
}

func (c *proxyCache) Get(target *url.URL, transport http.RoundTripper) *httputil.ReverseProxy {
	hostKey := targetHostKey(target)

	c.mu.RLock()
	proxy, ok := c.proxies[hostKey]
	c.mu.RUnlock()
	if ok {
		return proxy
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if proxy, ok = c.proxies[hostKey]; ok {
		return proxy
	}

	proxy = newReverseProxy(target, transport, c.metrics)
	c.proxies[hostKey] = proxy
	return proxy
}

func newReverseProxy(target *url.URL, transport http.RoundTripper, metrics *proxyMetrics) *httputil.ReverseProxy {
	targetCopy := &url.URL{Scheme: target.Scheme, Host: target.Host}
	proxy := httputil.NewSingleHostReverseProxy(targetCopy)
	proxy.Transport = transport

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if req.Header.Get("X-Forwarded-Proto") == "" {
			if req.TLS != nil {
				req.Header.Set("X-Forwarded-Proto", "https")
			} else {
				req.Header.Set("X-Forwarded-Proto", "http")
			}
		}
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		metrics.recordError(targetCopy.String(), err)
		log.Printf("gateway.proxy error target=%s err=%v", targetCopy, err)
		http.Error(w, "upstream error", http.StatusBadGateway)
	}

	return proxy
}

func targetHostKey(u *url.URL) string {
	var b strings.Builder
	b.Grow(len(u.Scheme) + len(u.Host) + 3)
	b.WriteString(u.Scheme)
	b.WriteString("://")
	b.WriteString(u.Host)
	return b.String()
}

func (m *proxyMetrics) recordError(target string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastTarget = target
	m.lastError = err
}

func defaultTransport() http.RoundTripper {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// loggingMiddleware wraps handlers to log requests with latency and status code.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)

		reqID := middleware.GetRequestID(r.Context())
		log.Printf("gateway.request id=%s method=%s path=%s status=%d duration=%s upstream=%s",
			reqID, r.Method, r.URL.Path, lrw.status, time.Since(start), w.Header().Get("X-Upstream-Service"))
	})
}

// responseRecorder records HTTP status code for logging purposes.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	// Ensure status defaults to 200 when Write is called without WriteHeader.
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// readinessHandler reports whether at least one instance per service exists.
func readinessHandler(reg *serviceRegistry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := reg.checkReady(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (r *serviceRegistry) checkReady() error {
	r.RLock()
	defer r.RUnlock()
	for name, entry := range r.entries {
		if entry.readyInstances() == 0 {
			return fmt.Errorf("service %s has no instances", name)
		}
	}
	return nil
}

func (e *serviceEntry) readyInstances() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.instances)
}

func (h *gatewayHandler) routesSnapshot() []route {
	return append([]route(nil), h.routes...)
}

// debugRoutesHandler dumps routing mapping for debugging purposes.
func debugRoutesHandler(handler *gatewayHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sb strings.Builder
		sb.WriteString("prefix -> service\n")
		for _, route := range handler.routesSnapshot() {
			sb.WriteString(route.prefix)
			sb.WriteString(" -> ")
			sb.WriteString(route.service.name)
			sb.WriteString("\n")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(sb.String()))
	})
}
