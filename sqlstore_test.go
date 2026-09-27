package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testPNG = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64))

// queryExecModes are the ways pgx can send a query. Supabase's transaction
// pooler can't keep prepared statements, so connecting through it takes exec or
// simple_protocol instead of the default.
var queryExecModes = []pgx.QueryExecMode{
	pgx.QueryExecModeCacheStatement,
	pgx.QueryExecModeExec,
	pgx.QueryExecModeSimpleProtocol,
}

func withQueryExecMode(mode pgx.QueryExecMode) func(*pgxpool.Config) {
	return func(config *pgxpool.Config) { config.ConnConfig.DefaultQueryExecMode = mode }
}

// testDatabase names a database of the test's own on the server
// TEST_DATABASE_URL points at, and returns a pool config for it and a function
// that creates it. The database is dropped when the test ends. Tests that use
// it are skipped when TEST_DATABASE_URL isn't set.
func testDatabase(t *testing.T) (*pgxpool.Config, func()) {
	t.Helper()
	serverURL := os.Getenv("TEST_DATABASE_URL")
	if serverURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping the Postgres tests")
	}

	config, err := pgxpool.ParseConfig(serverURL)
	if err != nil {
		t.Fatalf("parsing TEST_DATABASE_URL: %v", err)
	}
	name := fmt.Sprintf("journal_test_%x", rand.Uint64())
	config.ConnConfig.Database = name

	admin := func(sql string) error {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, serverURL)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()
		_, err = conn.Exec(ctx, sql)
		return err
	}
	t.Cleanup(func() {
		if err := admin("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"); err != nil {
			t.Errorf("dropping test database %s: %v", name, err)
		}
	})

	return config, func() {
		t.Helper()
		if err := admin("CREATE DATABASE " + name); err != nil {
			t.Fatalf("creating test database %s: %v", name, err)
		}
	}
}

// newTestSQLStore returns a sqlStore on an empty database of the test's own.
func newTestSQLStore(t *testing.T, configure ...func(*pgxpool.Config)) *sqlStore {
	t.Helper()
	config, create := testDatabase(t)
	create()
	for _, apply := range configure {
		apply(config)
	}

	db, err := newSQLStore(config)
	if err != nil {
		t.Fatalf("newSQLStore failed: %v", err)
	}
	t.Cleanup(db.pool.Close)
	return db
}

// saveSampleJournal puts a couple of days' entries into a store, in the order
// the app saves them: each note is saved before its analysis arrives, and a
// topic's synthesis is rewritten as its notes come in.
func saveSampleJournal(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	day1 := time.Date(2026, 9, 14, 9, 0, 0, 0, time.Local)
	day2 := day1.AddDate(0, 0, 1)

	note := func(key EntryKey, content string) int64 {
		t.Helper()
		id, err := store.SaveRawEntry(ctx, key, content)
		if err != nil {
			t.Fatalf("SaveRawEntry(%q) failed: %v", content, err)
		}
		return id
	}
	analyse := func(key EntryKey, id int64, analysis map[string]interface{}) {
		t.Helper()
		if err := store.SaveAnalysis(ctx, key, id, analysis); err != nil {
			t.Fatalf("SaveAnalysis failed: %v", err)
		}
	}
	synthesise := func(key EntryKey, analysis map[string]interface{}) {
		t.Helper()
		if err := store.ReplaceTopicAnalysis(ctx, key, analysis); err != nil {
			t.Fatalf("ReplaceTopicAnalysis failed: %v", err)
		}
	}
	photo := func(key EntryKey, at time.Time) {
		t.Helper()
		if _, err := store.AddPhoto(ctx, key, at, key.Topic+" "+at.Format("15:04"), testPNG); err != nil {
			t.Fatalf("AddPhoto failed: %v", err)
		}
	}

	// Journal entries, each analysed on its own and merged into the day.
	journal1 := EntryKey{Type: "journal", Day: day1}
	analyse(journal1, note(journal1, "first entry"), map[string]interface{}{
		"emotional_checkin": "fine",
		"happy_things":      []interface{}{"sunshine", "coffee"},
	})
	analyse(journal1, note(journal1, "second entry\nover two lines"), map[string]interface{}{
		"emotional_checkin": "better",
		"stressful_things":  []interface{}{"traffic"},
		"focus_items":       []interface{}{"take breaks"},
	})
	journal2 := EntryKey{Type: "journal", Day: day2}
	analyse(journal2, note(journal2, "the next day"), map[string]interface{}{
		"emotional_checkin": "rested",
	})

	// A talk: notes, photos (two in the same second), and a synthesis that is
	// replaced as each note arrives.
	talk := EntryKey{Type: "notes", Day: day1, Topic: "Scaling Postgres"}
	note(talk, "they shard by tenant")
	synthesise(talk, map[string]interface{}{
		"summary": "a talk about sharding",
		"notes":   []interface{}{"shard by tenant"},
	})
	slide := day1.Add(12 * time.Minute)
	photo(talk, slide)
	photo(talk, slide)
	note(talk, "wal shipping is async")
	synthesise(talk, map[string]interface{}{
		"summary": "a talk about sharding and replication",
		"notes":   []interface{}{"shard by tenant", "wal shipping is async"},
	})

	// A second talk, then a loose thought and a photo outside any topic.
	rust := EntryKey{Type: "notes", Day: day1, Topic: "Rust in Production"}
	note(rust, "no gc pauses")
	loose := EntryKey{Type: "notes", Day: day1}
	analyse(loose, note(loose, "a loose thought"), map[string]interface{}{
		"summary": "loose",
		"notes":   []interface{}{"one", "two"},
	})
	photo(loose, day1.Add(time.Hour))

	keynote := EntryKey{Type: "notes", Day: day2, Topic: "Keynote"}
	note(keynote, "welcome")
}

// The frontend parses GET /api/entries the same way whichever store answers
// it, so the database has to give back the document the file store writes.
func TestSQLStoreRendersLikeFileStore(t *testing.T) {
	for _, format := range []string{"markdown", "org"} {
		for _, mode := range queryExecModes {
			t.Run(format+"/"+mode.String(), func(t *testing.T) {
				setupStorageTest(t, format)
				db := newTestSQLStore(t, withQueryExecMode(mode))
				ctx := context.Background()

				saveSampleJournal(t, fileStore{})
				saveSampleJournal(t, db)

				for _, entryType := range []string{"journal", "notes"} {
					want, err := fileStore{}.GetEntries(ctx, entryType)
					if err != nil {
						t.Fatalf("file GetEntries failed: %v", err)
					}
					got, err := db.GetEntries(ctx, entryType)
					if err != nil {
						t.Fatalf("database GetEntries failed: %v", err)
					}
					if got != want {
						t.Errorf("%s entries differ.\ndatabase:\n%s\nfile:\n%s", entryType, got, want)
					}
				}

				day1 := time.Date(2026, 9, 14, 0, 0, 0, 0, time.Local)
				for _, day := range []time.Time{day1, day1.AddDate(0, 0, 1), day1.AddDate(0, 0, 2)} {
					want, err := fileStore{}.ListTopics(ctx, "notes", day)
					if err != nil {
						t.Fatalf("file ListTopics failed: %v", err)
					}
					got, err := db.ListTopics(ctx, "notes", day)
					if err != nil {
						t.Fatalf("database ListTopics failed: %v", err)
					}
					if !slices.Equal(got, want) {
						t.Errorf("ListTopics(%s) = %q, file store has %q", day.Format("2006-01-02"), got, want)
					}
				}

				for _, topic := range []string{"Scaling Postgres", "Rust in Production", "No Such Talk"} {
					key := EntryKey{Type: "notes", Day: day1, Topic: topic}
					want, err := fileStore{}.GetTopicNotes(ctx, key)
					if err != nil {
						t.Fatalf("file GetTopicNotes failed: %v", err)
					}
					got, err := db.GetTopicNotes(ctx, key)
					if err != nil {
						t.Fatalf("database GetTopicNotes failed: %v", err)
					}
					if got != want {
						t.Errorf("GetTopicNotes(%q) = %q, file store has %q", topic, got, want)
					}
				}
			})
		}
	}
}

// The synthesis of a topic replaces the one before; it doesn't stack up.
func TestSQLStoreReplacesTopicSynthesis(t *testing.T) {
	setupStorageTest(t, "markdown")
	db := newTestSQLStore(t)
	saveSampleJournal(t, db)

	content, err := db.GetEntries(context.Background(), "notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Contains(content, "a talk about sharding\n") {
		t.Errorf("superseded synthesis still shown:\n%s", content)
	}
	if !strings.Contains(content, "a talk about sharding and replication") {
		t.Errorf("latest synthesis missing:\n%s", content)
	}
	if strings.Count(content, "- shard by tenant") != 1 {
		t.Errorf("synthesis bullets duplicated:\n%s", content)
	}
}

func TestSQLStorePhotos(t *testing.T) {
	setupStorageTest(t, "markdown")
	db := newTestSQLStore(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 15, 9, 12, 0, 0, time.Local)
	key := EntryKey{Type: "notes", Day: at, Topic: "Scaling Postgres"}

	// The title slide, snapped before any notes are taken.
	first, err := db.AddPhoto(ctx, key, at, "Scaling Postgres 09:12", testPNG)
	if err != nil {
		t.Fatalf("AddPhoto failed: %v", err)
	}
	if want := "images/2026-09-15/091200-scaling-postgres.png"; first != want {
		t.Errorf("photo path = %q, want %q", first, want)
	}
	second, err := db.AddPhoto(ctx, key, at, "Scaling Postgres 09:12", testPNG)
	if err != nil {
		t.Fatalf("second AddPhoto failed: %v", err)
	}
	if want := "images/2026-09-15/091200-scaling-postgres-1.png"; second != want {
		t.Errorf("second photo in the same second got path %q, want %q", second, want)
	}
	if _, err := db.SaveRawEntry(ctx, key, "they shard by tenant"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	photo, err := db.GetPhoto(ctx, first)
	if err != nil {
		t.Fatalf("GetPhoto failed: %v", err)
	}
	if !bytes.Equal(photo.Data, testPNG) || photo.ContentType != "image/png" || photo.Name != "091200-scaling-postgres.png" {
		t.Errorf("GetPhoto = %q (%s, %d bytes), want the uploaded PNG", photo.Name, photo.ContentType, len(photo.Data))
	}
	for _, missing := range []string{"images/2026-09-15/nothing.png", "../journal.md", "images/../journal.md", "journal.md"} {
		if _, err := db.GetPhoto(ctx, missing); !errors.Is(err, errPhotoNotFound) {
			t.Errorf("GetPhoto(%q) error = %v, want errPhotoNotFound", missing, err)
		}
	}

	if _, err := db.AddPhoto(ctx, key, at, "script", []byte("#!/bin/sh\necho hi\n")); !errors.Is(err, errUnsupportedPhoto) {
		t.Errorf("AddPhoto of a script: error = %v, want errUnsupportedPhoto", err)
	}

	content, err := db.GetEntries(ctx, "notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Count(content, "![Scaling Postgres 09:12](") != 2 {
		t.Errorf("expected exactly the two photos to be linked:\n%s", content)
	}
	photosIdx := strings.Index(content, "#### Photos")
	rawIdx := strings.Index(content, "#### Raw Input")
	if photosIdx == -1 || rawIdx == -1 || photosIdx > rawIdx || !strings.Contains(content, "they shard by tenant") {
		t.Errorf("expected the photos above the topic's notes:\n%s", content)
	}
}

// An entry saved late in the evening belongs to that evening, whatever time
// zone the database session is in.
func TestSQLStoreKeepsTheLocalDay(t *testing.T) {
	for _, mode := range queryExecModes {
		t.Run(mode.String(), func(t *testing.T) {
			setupStorageTest(t, "markdown")
			db := newTestSQLStore(t, withQueryExecMode(mode), func(config *pgxpool.Config) {
				config.ConnConfig.RuntimeParams["timezone"] = "Pacific/Kiritimati" // UTC+14
			})
			ctx := context.Background()

			evening := time.Date(2026, 9, 14, 23, 30, 0, 0, time.FixedZone("UTC-10", -10*60*60))
			key := EntryKey{Type: "notes", Day: evening, Topic: "Late talk"}
			if _, err := db.SaveRawEntry(ctx, key, "a late note"); err != nil {
				t.Fatalf("SaveRawEntry failed: %v", err)
			}

			topics, err := db.ListTopics(ctx, "notes", evening)
			if err != nil {
				t.Fatalf("ListTopics failed: %v", err)
			}
			if !slices.Equal(topics, []string{"Late talk"}) {
				t.Errorf("ListTopics = %q, want the late talk", topics)
			}
			content, err := db.GetEntries(ctx, "notes")
			if err != nil {
				t.Fatalf("GetEntries failed: %v", err)
			}
			if !strings.HasPrefix(content, "## 2026-09-14 Mon\n") {
				t.Errorf("expected the note filed under Monday the 14th:\n%s", content)
			}
		})
	}
}

// The database may not be reachable yet when the app starts. The migrations
// then run on the first request that finds it up.
func TestSQLStoreRetriesMigrationsUntilTheDatabaseIsUp(t *testing.T) {
	setupStorageTest(t, "markdown")
	config, create := testDatabase(t)
	db, err := newSQLStore(config)
	if err != nil {
		t.Fatalf("newSQLStore failed: %v", err)
	}
	t.Cleanup(db.pool.Close)
	ctx := context.Background()
	key := EntryKey{Type: "journal", Day: time.Now()}

	if _, err := db.SaveRawEntry(ctx, key, "too early"); err == nil {
		t.Fatal("expected saving to fail while the database doesn't exist")
	}

	create()
	if _, err := db.SaveRawEntry(ctx, key, "an entry"); err != nil {
		t.Fatalf("SaveRawEntry failed once the database was up: %v", err)
	}
	content, err := db.GetEntries(ctx, "journal")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if !strings.Contains(content, "an entry") || strings.Contains(content, "too early") {
		t.Errorf("unexpected entries:\n%s", content)
	}
}

// Machines booting at the same time all migrate the same database; each
// migration still runs exactly once.
func TestMigrationsApplyOnce(t *testing.T) {
	config, create := testDatabase(t)
	create()
	ctx := context.Background()

	stores := make([]*sqlStore, 4)
	for i := range stores {
		db, err := newSQLStore(config.Copy())
		if err != nil {
			t.Fatalf("newSQLStore failed: %v", err)
		}
		t.Cleanup(db.pool.Close)
		stores[i] = db
	}

	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for i, db := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = db.ready(ctx)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("store %d failed to migrate: %v", i, err)
		}
	}

	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("loadMigrations failed: %v", err)
	}
	rows, err := stores[0].pool.Query(ctx, "SELECT version FROM journal.schema_migrations ORDER BY version")
	if err != nil {
		t.Fatalf("reading schema_migrations failed: %v", err)
	}
	applied, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		t.Fatalf("reading schema_migrations failed: %v", err)
	}
	var want []int
	for _, m := range migrations {
		want = append(want, m.version)
	}
	if !slices.Equal(applied, want) {
		t.Errorf("applied migrations = %v, want %v", applied, want)
	}

	// Only the role that owns the tables, which the app connects as, may read
	// them, even if the schema is ever exposed through Supabase's REST API.
	rows, err = stores[0].pool.Query(ctx, `
		SELECT relname FROM pg_class
		WHERE relnamespace = 'journal'::regnamespace AND relkind = 'r'
		  AND relname <> 'schema_migrations' AND NOT relrowsecurity`)
	if err != nil {
		t.Fatalf("reading pg_class failed: %v", err)
	}
	unprotected, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("reading pg_class failed: %v", err)
	}
	if len(unprotected) > 0 {
		t.Errorf("tables without row level security: %v", unprotected)
	}
}
