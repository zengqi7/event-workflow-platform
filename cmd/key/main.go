package main

import (
	"context"
	"database/sql"
	mysqlrepo "event-workflow-platform/internal/repo/mysql"
	"event-workflow-platform/migrations"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	tenant := flag.Uint64("tenant", 0, "tenant ID")
	subject := flag.String("subject", "", "key subject / revoke actor")
	scopes := flag.String("scopes", "events:write,state:read", "comma separated scopes")
	ttl := flag.Duration("ttl", 30*24*time.Hour, "credential lifetime")
	revoke := flag.Uint64("revoke", 0, "key ID to revoke")
	flag.Parse()
	if *tenant == 0 || *subject == "" || os.Getenv("MYSQL_DSN") == "" {
		log.Fatal("tenant, subject and MYSQL_DSN required")
	}
	db, err := sql.Open("mysql", os.Getenv("MYSQL_DSN"))
	if err != nil {
		log.Fatal("cannot open database")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrations.Verify(ctx, db); err != nil {
		log.Fatal(err)
	}
	store := mysqlrepo.NewStore(db)
	if *revoke != 0 {
		if err := store.RevokeKey(ctx, *tenant, *revoke, *subject); err != nil {
			log.Fatal("revoke failed")
		}
		fmt.Println("revoked")
		return
	}
	id, token, err := store.CreateKey(ctx, *tenant, *subject, strings.Split(*scopes, ","), time.Now().Add(*ttl))
	if err != nil {
		log.Fatal("key creation failed")
	}
	fmt.Printf("key_id=%d\ntoken=%s\n", id, token)
}
