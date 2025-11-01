package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// serviceRegistry maintains service instance data discovered from etcd.
// Each configured service gets an entry that tracks live endpoints and
// powers route selection in the HTTP layer.
type serviceRegistry struct {
	client  *clientv3.Client
	entries map[string]*serviceEntry
	sync.RWMutex
	wg sync.WaitGroup
}

// serviceEntry stores the known instances for a single logical service.
// Instances are keyed by etcd registration keys and updated via watch.
type serviceEntry struct {
	name            string
	scheme          string
	discoveryPrefix string
	prefixes        []string

	mu        sync.RWMutex
	instances map[string]*url.URL
	seq       uint32
}

// instancePayload describes etcd registration data when JSON encoded.
// Some registrars may publish structured payloads instead of plain URLs.
type instancePayload struct {
	Address string            `json:"address"`
	Scheme  string            `json:"scheme"`
	Meta    map[string]string `json:"metadata"`
}

func newServiceRegistry(cli *clientv3.Client, services []ServiceConfig) (*serviceRegistry, error) {
	reg := &serviceRegistry{
		client:  cli,
		entries: make(map[string]*serviceEntry),
	}

	for _, svc := range services {
		entry := &serviceEntry{
			name:            svc.Name,
			scheme:          svc.Scheme,
			discoveryPrefix: svc.DiscoveryPrefix,
			prefixes:        append([]string(nil), svc.Prefixes...),
			instances:       make(map[string]*url.URL),
		}

		reg.entries[svc.Name] = entry
	}

	return reg, nil
}

// start launches watch loops for each service entry. Every entry keeps
// watching its etcd prefix until the provided context is canceled.
func (r *serviceRegistry) start(ctx context.Context) {
	for _, entry := range r.entries {
		entry := entry

		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			entry.run(ctx, r.client)
		}()
	}
}

func (r *serviceRegistry) listEntries() []*serviceEntry {
	r.RLock()
	defer r.RUnlock()

	entries := make([]*serviceEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	return entries
}

func (r *serviceRegistry) close() error {
	r.wg.Wait()
	return r.client.Close()
}

// run bootstraps existing entries and keeps watching for changes to the
// etcd prefix associated with the service.
func (e *serviceEntry) run(ctx context.Context, cli *clientv3.Client) {
	if err := e.bootstrap(ctx, cli); err != nil {
		log.Printf("gateway.discovery service=%s bootstrap failed: %v", e.name, err)
	}

	watcher := clientv3.NewWatcher(cli)
	defer watcher.Close()

	watchCh := watcher.Watch(ctx, e.discoveryPrefix, clientv3.WithPrefix())
	for {
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-watchCh:
			if !ok {
				time.Sleep(500 * time.Millisecond)
				watchCh = watcher.Watch(ctx, e.discoveryPrefix, clientv3.WithPrefix())
				continue
			}

			if resp.Err() != nil {
				log.Printf("gateway.discovery service=%s watch error: %v", e.name, resp.Err())
				continue
			}

			for _, ev := range resp.Events {
				switch ev.Type {
				case clientv3.EventTypePut:
					if err := e.handlePut(ev); err != nil {
						log.Printf("gateway.discovery service=%s handle put error: %v", e.name, err)
					}
				case clientv3.EventTypeDelete:
					e.handleDelete(string(ev.Kv.Key))
				}
			}
		}
	}
}

// bootstrap fetches existing registrations to seed the in-memory table
// before the watch loop starts consuming incremental updates.
func (e *serviceEntry) bootstrap(ctx context.Context, cli *clientv3.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := cli.Get(ctx, e.discoveryPrefix, clientv3.WithPrefix())
	if err != nil {
		return err
	}

	for _, kv := range resp.Kvs {
		if err := e.storeInstance(string(kv.Key), kv.Value); err != nil {
			log.Printf("gateway.discovery service=%s bootstrap parse key=%s error=%v", e.name, string(kv.Key), err)
		}
	}

	return nil
}

func (e *serviceEntry) handlePut(ev *clientv3.Event) error {
	return e.storeInstance(string(ev.Kv.Key), ev.Kv.Value)
}

func (e *serviceEntry) storeInstance(key string, value []byte) error {
	target, err := parseInstanceTarget(value, e.scheme)
	if err != nil {
		return err
	}
	e.setInstance(key, target)
	return nil
}

func (e *serviceEntry) setInstance(key string, target *url.URL) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.instances[key] = target
}

func (e *serviceEntry) handleDelete(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.instances, key)
}

func (e *serviceEntry) pickEndpoint() (*url.URL, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(e.instances) == 0 {
		return nil, errors.New("no available instances")
	}

	// Iterate the map into a slice each time to avoid holding onto a
	// potentially stale reference after the read lock is released.
	targets := make([]*url.URL, 0, len(e.instances))
	for _, target := range e.instances {
		targets = append(targets, target)
	}

	// Basic round-robin across the active endpoints using an atomic
	// counter, which is good enough for the initial implementation.
	idx := atomic.AddUint32(&e.seq, 1)
	chosen := targets[int(idx-1)%len(targets)]
	clone := *chosen
	return &clone, nil
}

func parseInstanceTarget(raw []byte, defaultScheme string) (*url.URL, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil, fmt.Errorf("empty instance value")
	}

	if strings.HasPrefix(s, "{") {
		// JSON payload support keeps compatibility with registrars that
		// publish structured metadata along with the endpoint address.
		var payload instancePayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, fmt.Errorf("decode payload: %w", err)
		}
		addr := strings.TrimSpace(payload.Address)
		if addr == "" {
			return nil, fmt.Errorf("payload missing address")
		}
		scheme := payload.Scheme
		if scheme == "" {
			scheme = defaultScheme
		}
		return buildURL(addr, scheme)
	}

	return buildURL(s, defaultScheme)
}

func buildURL(addr, scheme string) (*url.URL, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("empty address")
	}

	if !strings.Contains(addr, "://") {
		u := &url.URL{Scheme: scheme, Host: addr}
		return u, nil
	}

	u, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = scheme
	}
	return u, nil
}

func (e *serviceEntry) hasPrefix(path string) bool {
	for _, prefix := range e.prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (e *serviceEntry) listPrefixes() []string {
	return append([]string(nil), e.prefixes...)
}
