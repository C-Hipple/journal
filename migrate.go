package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationFiles are the schema migrations, applied in version order when the
// app starts against a database that hasn't run them yet. Name a new one
// NNNN_description.sql with the next number. Never edit one that has shipped:
// databases that already ran it won't run it again.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockKey names the advisory lock that stops two instances starting
// at once from applying the same migration twice. It spells "journal" in ASCII.
const migrationLockKey int64 = 0x6a6f75726e616c

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the migrations in fsys, ordered by version.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	files, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, err
	}

	migrations := make([]migration, 0, len(files))
	names := map[int]string{}
	for _, file := range files {
		name := path.Base(file)
		prefix, _, _ := strings.Cut(name, "_")
		version, err := strconv.Atoi(prefix)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migration %s: name must start with its version number, like 0001_", name)
		}
		if other, ok := names[version]; ok {
			return nil, fmt.Errorf("migrations %s and %s have the same version", other, name)
		}
		names[version] = name

		body, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(body)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// migrate applies the migrations the database hasn't run yet. They run in one
// transaction, so a failure leaves the schema as it was. That also means a
// migration can't use statements that refuse to run inside a transaction, such
// as CREATE INDEX CONCURRENTLY.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return err
	}

	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Released when the transaction ends. A session-level lock would
		// outlive it, and wouldn't hold through Supabase's transaction pooler.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockKey); err != nil {
			return fmt.Errorf("taking the migration lock: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			CREATE SCHEMA IF NOT EXISTS journal;
			CREATE TABLE IF NOT EXISTS journal.schema_migrations (
				version    INTEGER PRIMARY KEY,
				name       TEXT NOT NULL,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`); err != nil {
			return fmt.Errorf("creating journal.schema_migrations: %w", err)
		}

		rows, err := tx.Query(ctx, "SELECT version FROM journal.schema_migrations")
		if err != nil {
			return err
		}
		versions, err := pgx.CollectRows(rows, pgx.RowTo[int])
		if err != nil {
			return fmt.Errorf("reading journal.schema_migrations: %w", err)
		}
		applied := make(map[int]bool, len(versions))
		for _, version := range versions {
			applied[version] = true
		}

		for _, m := range migrations {
			if applied[m.version] {
				continue
			}
			log.Printf("Applying database migration %s", m.name)
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return fmt.Errorf("applying migration %s: %w", m.name, err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO journal.schema_migrations (version, name) VALUES ($1, $2)", m.version, m.name); err != nil {
				return fmt.Errorf("recording migration %s: %w", m.name, err)
			}
		}
		return nil
	})
}
