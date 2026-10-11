package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"event-workflow-platform/internal/infra"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	"event-workflow-platform/internal/service/notifier"
	workertransport "event-workflow-platform/internal/transport/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown, err := infra.SetupTracing(ctx, "event-workflow-worker")
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(flush)
	}()
	cfg := infra.LoadConfig()
	if cfg.MySQLDSN == "" {
		log.Fatal("MYSQL_DSN is required")
	}

	log.Printf("app=%s env=%s worker=starting", cfg.AppName, cfg.Environment)

	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	store := mysqlrepo.NewStore(db)
	policy := notifier.Policy{AllowedHosts: strings.FieldsFunc(os.Getenv("WEBHOOK_ALLOWED_HOSTS"), func(r rune) bool { return r == ',' })}
	policy.Development = (cfg.Environment == "dev" || cfg.Environment == "docker") && os.Getenv("WEBHOOK_ALLOW_DEVELOPMENT") == "true"
	executor := notifier.NewWithPolicy(5*time.Second, policy, notifier.DirectorySecrets{Root: os.Getenv("WEBHOOK_SECRET_DIR")})
	runner := workertransport.NewRunner(store, executor)
	concurrency := 4
	if raw := os.Getenv("WORKER_CONCURRENCY"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 16 {
			log.Fatal("WORKER_CONCURRENCY must be 1..16")
		}
		concurrency = parsed
	}
	metricsAddr := os.Getenv("WORKER_METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = "127.0.0.1:9091"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		scrape, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		queue, err := store.QueueMetrics(scrape)
		if err != nil {
			http.Error(w, "queue metrics unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(infra.RenderMetrics() + queue))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		check, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if store.Ping(check) != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		w.WriteHeader(200)
	})
	metricsServer := &http.Server{Addr: metricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("metrics server failed: %v", err)
			stop()
		}
	}()
	defer func() {
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsServer.Shutdown(drain)
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var wg sync.WaitGroup
		for range concurrency {
			wg.Go(func() {
				if err := runner.PollOnce(ctx, 5); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("worker poll failed: %v", err)
				}
			})
		}
		wg.Wait()
	}
}
