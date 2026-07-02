// migrate applies backend/migrations/*.sql in filename order, exactly once
// each, tracked in schema_migrations. Runs as the Fly release command.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/sushan/longevity/internal/config"
)

func main() {
	cfg := config.Load()
	dir := "migrations"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		log.Fatalf("schema_migrations: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		log.Fatalf("glob: %v", err)
	}
	sort.Strings(files)

	for _, f := range files {
		name := filepath.Base(f)
		var applied bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = $1)`, name,
		).Scan(&applied); err != nil {
			log.Fatalf("%s: check: %v", name, err)
		}
		if applied {
			continue
		}
		sql, err := os.ReadFile(f)
		if err != nil {
			log.Fatalf("%s: read: %v", name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			log.Fatalf("%s: begin: %v", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("%s: apply: %v", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			log.Fatalf("%s: record: %v", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			log.Fatalf("%s: commit: %v", name, err)
		}
		log.Printf("applied %s", name)
	}
	log.Printf("migrations up to date (%d files)", len(files))
}
