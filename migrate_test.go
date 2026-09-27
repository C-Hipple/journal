package main

import (
	"testing"
	"testing/fstest"
)

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("the embedded migrations don't load: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations embedded")
	}

	// Ordered by version number, not by name.
	migrations, err = loadMigrations(fstest.MapFS{
		"migrations/10_later.sql": {Data: []byte("SELECT 10")},
		"migrations/9_sooner.sql": {Data: []byte("SELECT 9")},
	})
	if err != nil {
		t.Fatalf("loadMigrations failed: %v", err)
	}
	if len(migrations) != 2 || migrations[0].version != 9 || migrations[1].version != 10 || migrations[0].sql != "SELECT 9" {
		t.Errorf("loadMigrations = %+v, want version 9 then 10", migrations)
	}

	for name, fsys := range map[string]fstest.MapFS{
		"no version":     {"migrations/create_tables.sql": {}},
		"version zero":   {"migrations/0000_nothing.sql": {}},
		"shared version": {"migrations/0001_a.sql": {}, "migrations/1_b.sql": {}},
	} {
		if _, err := loadMigrations(fsys); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
