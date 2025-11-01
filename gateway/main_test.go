package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"

	"github.com/Zfyzfyzfy3/SmartMarketSystem/gateway/middleware"
)

func TestGatewayRoutesViaEtcd(t *testing.T) {
	etcdCfg := embed.NewConfig()
	etcdCfg.Dir = t.TempDir()
	etcdCfg.Logger = "zap"
	etcdCfg.LogLevel = "error"

	clientURL := mustFreeURL()
	peerURL := mustFreeURL()
	etcdCfg.ListenClientUrls = []url.URL{*clientURL}
	etcdCfg.AdvertiseClientUrls = []url.URL{*clientURL}
	etcdCfg.ListenPeerUrls = []url.URL{*peerURL}
	etcdCfg.AdvertisePeerUrls = []url.URL{*peerURL}
	etcdCfg.InitialCluster = fmt.Sprintf("%s=%s", etcdCfg.Name, etcdCfg.AdvertisePeerUrls[0].String())

	etcd, err := embed.StartEtcd(etcdCfg)
	if err != nil {
		t.Fatalf("start embedded etcd: %v", err)
	}
	defer etcd.Close()

	select {
	case <-etcd.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		t.Fatal("embedded etcd failed to start")
	}

	endpoint := "http://" + etcd.Clients[0].Addr().String()
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("create etcd client: %v", err)
	}
	defer cli.Close()

	backendReqID := make(chan string, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendReqID <- r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := "/services/user/instance-1"
	if _, err := cli.Put(ctx, key, backendURL.String()); err != nil {
		t.Fatalf("put service endpoint: %v", err)
	}

	svcCfg := []ServiceConfig{{
		Name:            "user-service",
		Scheme:          backendURL.Scheme,
		DiscoveryPrefix: "/services/user",
		Prefixes:        []string{"/api/v1/users"},
	}}

	reg, err := newServiceRegistry(cli, svcCfg)
	if err != nil {
		t.Fatalf("init registry: %v", err)
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	reg.start(runCtx)

	waitUntilReady(t, reg)

	handler := newGatewayHandler(reg.listEntries())
	gateway := httptest.NewServer(middleware.RequestID(loggingMiddleware(handler)))
	defer gateway.Close()

	resp, err := http.Get(gateway.URL + "/api/v1/users")
	if err != nil {
		t.Fatalf("request gateway: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("unexpected body: %s", string(body))
	}

	if got := <-backendReqID; got == "" {
		t.Fatalf("backend did not receive request id header")
	}

	if resp.Header.Get("X-Upstream-Service") != "user-service" {
		t.Fatalf("missing upstream header: %v", resp.Header.Get("X-Upstream-Service"))
	}

	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatalf("gateway response missing request id")
	}

	runCancel()
	if err := reg.close(); err != nil {
		t.Fatalf("close registry: %v", err)
	}
}

func waitUntilReady(t *testing.T, reg *serviceRegistry) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := reg.checkReady(); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("registry did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func mustFreeURL() *url.URL {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	u, err := url.Parse("http://" + addr)
	if err != nil {
		panic(err)
	}
	return u
}
