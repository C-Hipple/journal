package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupStorageTest points storage at a temp directory with git disabled.
func setupStorageTest(t *testing.T, format string) {
	t.Helper()
	t.Chdir(t.TempDir())
	journalFormat = format
	gitUsername = ""
	gitRepoName = ""
	githubToken = ""
}

func TestRawInputSavedBeforeAnalysis(t *testing.T) {
	setupStorageTest(t, "markdown")
	dateHeader := time.Now().Format(getDateHeaderFormat())

	if err := SaveRawEntry("journal", "today was a good day", dateHeader); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	content, err := GetEntries("journal")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if !strings.Contains(content, dateHeader) {
		t.Errorf("expected date header %q in:\n%s", dateHeader, content)
	}
	if !strings.Contains(content, "### Raw Input") || !strings.Contains(content, "today was a good day") {
		t.Errorf("raw input not persisted:\n%s", content)
	}

	// Analysis arrives later and merges into the same entry.
	analysis := map[string]interface{}{
		"emotional_checkin": "feeling good",
		"happy_things":      []interface{}{"sunshine", "coffee"},
	}
	if err := SaveEntry("journal", analysis, dateHeader); err != nil {
		t.Fatalf("SaveEntry failed: %v", err)
	}

	content, err = GetEntries("journal")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Count(content, dateHeader) != 1 {
		t.Errorf("expected a single date header, got:\n%s", content)
	}
	if !strings.Contains(content, "### General Emotional Checkin") || !strings.Contains(content, "- sunshine") {
		t.Errorf("analysis sections missing:\n%s", content)
	}

	// Raw Input must stay the last section so the frontend parser doesn't
	// swallow analysis sections into the raw text.
	rawIdx := strings.Index(content, "### Raw Input")
	for _, section := range []string{"### General Emotional Checkin", "### Things that made me happy"} {
		if idx := strings.Index(content, section); idx > rawIdx {
			t.Errorf("section %q appears after Raw Input:\n%s", section, content)
		}
	}
}

func TestSecondEntrySameDayMerges(t *testing.T) {
	setupStorageTest(t, "markdown")
	dateHeader := time.Now().Format(getDateHeaderFormat())

	if err := SaveRawEntry("journal", "first entry", dateHeader); err != nil {
		t.Fatalf("first SaveRawEntry failed: %v", err)
	}
	if err := SaveEntry("journal", map[string]interface{}{"emotional_checkin": "fine"}, dateHeader); err != nil {
		t.Fatalf("first SaveEntry failed: %v", err)
	}
	if err := SaveRawEntry("journal", "second entry", dateHeader); err != nil {
		t.Fatalf("second SaveRawEntry failed: %v", err)
	}
	if err := SaveEntry("journal", map[string]interface{}{"emotional_checkin": "better"}, dateHeader); err != nil {
		t.Fatalf("second SaveEntry failed: %v", err)
	}

	content, err := GetEntries("journal")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Count(content, dateHeader) != 1 {
		t.Errorf("expected a single date header, got:\n%s", content)
	}
	for _, want := range []string{"first entry", "second entry", "fine", "better"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}
	rawIdx := strings.Index(content, "### Raw Input")
	if idx := strings.Index(content, "better"); idx > rawIdx {
		t.Errorf("merged analysis appears after Raw Input:\n%s", content)
	}
}

func TestRawInputOrgFormat(t *testing.T) {
	setupStorageTest(t, "org")
	dateHeader := time.Now().Format(getDateHeaderFormat())

	if err := SaveRawEntry("journal", "org mode entry", dateHeader); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveEntry("journal", map[string]interface{}{"emotional_checkin": "calm"}, dateHeader); err != nil {
		t.Fatalf("SaveEntry failed: %v", err)
	}

	content, err := GetEntries("journal")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	rawIdx := strings.Index(content, "** Raw Input")
	if rawIdx == -1 {
		t.Fatalf("missing raw input section:\n%s", content)
	}
	if idx := strings.Index(content, "** General Emotional Checkin"); idx == -1 || idx > rawIdx {
		t.Errorf("analysis section missing or after Raw Input:\n%s", content)
	}
}

func TestSaveEntryDoesNotLeaveTempFile(t *testing.T) {
	setupStorageTest(t, "markdown")

	if err := SaveRawEntry("journal", "hello", ""); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	cwd, _ := os.Getwd()
	matches, _ := filepath.Glob(filepath.Join(cwd, "*.tmp"))
	if len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}
