package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"

	"event-workflow-platform/internal/app/command"
	"event-workflow-platform/internal/infra"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	redisrepo "event-workflow-platform/internal/repo/redis"
	httptransport "event-workflow-platform/internal/transport/http"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdown, err := infra.SetupTracing(ctx, "event-workflow-api")
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

	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer redisClient.Close()

	store := mysqlrepo.NewStore(db)
	cache := redisrepo.NewCache(redisClient)
	ingestor := command.NewIngestEventCommand(store, cache)
	server := httptransport.NewServer(cfg.HTTPAddr, ingestor, store, store, cache)
	go func() {
		<-ctx.Done()
		drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(drain)
	}()

	log.Printf("app=%s env=%s http_addr=%s", cfg.AppName, cfg.Environment, cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
