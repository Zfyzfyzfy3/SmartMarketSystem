package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/Zfyzfyzfy3/SmartMarketSystem/gateway/middleware"
)

func main() {
	defaultConfig := filepath.Join("gateway", "config.yaml")
	configPath := flag.String("config", defaultConfig, "path to gateway config yaml")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:        cfg.Etcd.Endpoints,
		DialTimeout:      cfg.Etcd.DialTimeout,
		Username:         cfg.Etcd.Username,
		Password:         cfg.Etcd.Password,
		AutoSyncInterval: 30 * time.Second,
	})
	if err != nil {
		log.Fatalf("create etcd client: %v", err)
	}

	reg, err := newServiceRegistry(cli, cfg.Services)
	if err != nil {
		log.Fatalf("init registry: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reg.start(ctx)

	handler := newGatewayHandler(reg.listEntries())

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})
	mux.Handle("/_/ready", readinessHandler(reg))
	mux.Handle("/_/routes", debugRoutesHandler(handler))
	mux.Handle("/", middleware.RequestID(loggingMiddleware(handler)))

	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && err != http.ErrServerClosed {
			log.Printf("http shutdown error: %v", err)
		}
	}()

	log.Printf("gateway listening on %s", cfg.Server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}

	// Ensure background goroutines exit before cleanup.
	stop()

	if err := reg.close(); err != nil {
		log.Printf("registry close error: %v", err)
	}

	log.Printf("gateway stopped")
}
