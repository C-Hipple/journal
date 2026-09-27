package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// With git storage configured too, every write lands in both stores, and the
// app reads from the database.
func TestMirroredStoreWritesToBoth(t *testing.T) {
	setupStorageTest(t, "markdown")
	db := newTestSQLStore(t)
	store := mirroredStore{primary: db, mirror: fileStore{}}
	ctx := context.Background()

	saveSampleJournal(t, store)

	// An entry the database never saw stays out of what the app shows.
	if _, err := (fileStore{}).SaveRawEntry(ctx, EntryKey{Type: "notes", Day: time.Now()}, "only in git"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	for _, entryType := range []string{"journal", "notes"} {
		fromDB, err := db.GetEntries(ctx, entryType)
		if err != nil {
			t.Fatalf("GetEntries failed: %v", err)
		}
		fromFile, err := fileStore{}.GetEntries(ctx, entryType)
		if err != nil {
			t.Fatalf("GetEntries failed: %v", err)
		}
		if !strings.HasPrefix(fromFile, fromDB) {
			t.Errorf("the file store didn't get the same %s entries.\ndatabase:\n%s\nfile:\n%s", entryType, fromDB, fromFile)
		}
		got, err := store.GetEntries(ctx, entryType)
		if err != nil {
			t.Fatalf("GetEntries failed: %v", err)
		}
		if got != fromDB {
			t.Errorf("mirrored store served:\n%s\nwant the database's:\n%s", got, fromDB)
		}
	}
}

// When the git repo can't be reached, entries still save to the database: the
// mirror is best effort.
func TestMirroredStoreToleratesMirrorFailure(t *testing.T) {
	setupGitStorageTest(t, "markdown", filepath.Join(t.TempDir(), "missing", ".git"))
	db := newTestSQLStore(t)
	store := mirroredStore{primary: db, mirror: fileStore{}}
	ctx := context.Background()
	now := time.Now()
	key := EntryKey{Type: "notes", Day: now, Topic: "Keynote"}

	if _, err := store.SaveRawEntry(ctx, key, "a note"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := store.ReplaceTopicAnalysis(ctx, key, map[string]interface{}{"summary": "the opening talk"}); err != nil {
		t.Fatalf("ReplaceTopicAnalysis failed: %v", err)
	}
	relPath, err := store.AddPhoto(ctx, key, now, "Keynote", testPNG)
	if err != nil {
		t.Fatalf("AddPhoto failed: %v", err)
	}

	content, err := store.GetEntries(ctx, "notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	for _, want := range []string{"a note", "the opening talk", relPath} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
	if _, err := os.Stat(repoDir); !os.IsNotExist(err) {
		t.Errorf("expected nothing written to %s, got stat err %v", repoDir, err)
	}
}

// If the database write fails, so does the request, and the entry isn't left
// in the git repo alone.
func TestMirroredStoreFailsWithThePrimary(t *testing.T) {
	setupStorageTest(t, "markdown")
	// Nothing listens on port 1.
	db, err := openSQLStore("postgres://journal@127.0.0.1:1/journal")
	if err != nil {
		t.Fatalf("openSQLStore failed: %v", err)
	}
	t.Cleanup(db.pool.Close)
	store := mirroredStore{primary: db, mirror: fileStore{}}
	ctx := context.Background()
	key := EntryKey{Type: "journal", Day: time.Now()}

	if _, err := store.SaveRawEntry(ctx, key, "an entry"); err == nil {
		t.Error("expected SaveRawEntry to fail while the database is down")
	}
	if _, err := store.AddPhoto(ctx, key, time.Now(), "caption", testPNG); err == nil {
		t.Error("expected AddPhoto to fail while the database is down")
	}
	content, err := fileStore{}.GetEntries(ctx, "journal")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if content != "" {
		t.Errorf("expected nothing mirrored, got:\n%s", content)
	}
	if _, err := os.Stat(imagesDir); !os.IsNotExist(err) {
		t.Errorf("expected no photo mirrored, got stat err %v", err)
	}
}

func TestOpenStorePicksTheBackend(t *testing.T) {
	setupStorageTest(t, "markdown")
	const unreachable = "postgres://journal@127.0.0.1:1/journal"

	store, err := openStore("")
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	if _, ok := store.(fileStore); !ok {
		t.Errorf("without DATABASE_URL got %T, want fileStore", store)
	}

	// A database that is down at startup isn't fatal.
	store, err = openStore(unreachable)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	db, ok := store.(*sqlStore)
	if !ok {
		t.Fatalf("with DATABASE_URL got %T, want *sqlStore", store)
	}
	db.pool.Close()

	gitUsername, gitRepoName, githubToken = "someone", "journal-entries", "token"
	t.Cleanup(func() { gitUsername, gitRepoName, githubToken = "", "", "" })
	store, err = openStore(unreachable)
	if err != nil {
		t.Fatalf("openStore failed: %v", err)
	}
	mirrored, ok := store.(mirroredStore)
	if !ok {
		t.Fatalf("with DATABASE_URL and git storage got %T, want mirroredStore", store)
	}
	if _, ok := mirrored.primary.(*sqlStore); !ok {
		t.Errorf("mirrored store's primary is %T, want *sqlStore", mirrored.primary)
	}
	if _, ok := mirrored.mirror.(fileStore); !ok {
		t.Errorf("mirrored store's mirror is %T, want fileStore", mirrored.mirror)
	}
	mirrored.primary.(*sqlStore).pool.Close()

	if _, err := openStore("postgres://journal@127.0.0.1:1/journal?sslmode=nonsense"); err == nil {
		t.Error("expected a malformed DATABASE_URL to be rejected")
	}
}
