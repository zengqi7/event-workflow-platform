package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"event-workflow-platform/internal/infra"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	"event-workflow-platform/internal/service/notifier"
	workertransport "event-workflow-platform/internal/transport/worker"
)

func main() {
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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := runner.PollOnce(context.Background(), 20); err != nil {
			log.Printf("worker poll failed: %v", err)
		}
	}
}
