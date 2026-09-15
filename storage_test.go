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

	if err := SaveRawEntry("journal", "today was a good day", dateHeader, ""); err != nil {
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
	if err := SaveEntry("journal", analysis, dateHeader, ""); err != nil {
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

	if err := SaveRawEntry("journal", "first entry", dateHeader, ""); err != nil {
		t.Fatalf("first SaveRawEntry failed: %v", err)
	}
	if err := SaveEntry("journal", map[string]interface{}{"emotional_checkin": "fine"}, dateHeader, ""); err != nil {
		t.Fatalf("first SaveEntry failed: %v", err)
	}
	if err := SaveRawEntry("journal", "second entry", dateHeader, ""); err != nil {
		t.Fatalf("second SaveRawEntry failed: %v", err)
	}
	if err := SaveEntry("journal", map[string]interface{}{"emotional_checkin": "better"}, dateHeader, ""); err != nil {
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

	if err := SaveRawEntry("journal", "org mode entry", dateHeader, ""); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveEntry("journal", map[string]interface{}{"emotional_checkin": "calm"}, dateHeader, ""); err != nil {
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

	if err := SaveRawEntry("journal", "hello", "", ""); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	cwd, _ := os.Getwd()
	matches, _ := filepath.Glob(filepath.Join(cwd, "*.tmp"))
	if len(matches) > 0 {
		t.Errorf("temp files left behind: %v", matches)
	}
}

// A run of notes taken during one talk collects under a single topic heading
// instead of becoming a series of unrelated entries.
func TestTopicNotesCollectUnderOneHeading(t *testing.T) {
	setupStorageTest(t, "markdown")
	dateHeader := time.Now().Format(getDateHeaderFormat())
	const topic = "Scaling Postgres"

	for _, note := range []string{"they shard by tenant", "wal shipping is async"} {
		if err := SaveRawEntry("notes", note, dateHeader, topic); err != nil {
			t.Fatalf("SaveRawEntry failed: %v", err)
		}
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Count(content, "### Topic: "+topic) != 1 {
		t.Errorf("expected exactly one topic heading, got:\n%s", content)
	}
	if strings.Count(content, "#### Raw Input") != 1 {
		t.Errorf("expected notes to share one Raw Input section, got:\n%s", content)
	}
	for _, want := range []string{"they shard by tenant", "wal shipping is async"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing note %q in:\n%s", want, content)
		}
	}

	notes, err := GetTopicNotes("notes", dateHeader, topic)
	if err != nil {
		t.Fatalf("GetTopicNotes failed: %v", err)
	}
	if !strings.Contains(notes, "they shard by tenant") || !strings.Contains(notes, "wal shipping is async") {
		t.Errorf("GetTopicNotes should return every note, got:\n%s", notes)
	}

	topics, err := ListTopics("notes", dateHeader)
	if err != nil {
		t.Fatalf("ListTopics failed: %v", err)
	}
	if len(topics) != 1 || topics[0] != topic {
		t.Errorf("ListTopics = %v, want [%q]", topics, topic)
	}
}

// Re-synthesising a topic replaces its previous summary rather than stacking a
// second one underneath, and leaves the raw notes alone.
func TestReplaceTopicAnalysisSupersedesPreviousSynthesis(t *testing.T) {
	setupStorageTest(t, "markdown")
	dateHeader := time.Now().Format(getDateHeaderFormat())
	const topic = "Scaling Postgres"

	if err := SaveRawEntry("notes", "first note", dateHeader, topic); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := ReplaceTopicAnalysis("notes", map[string]interface{}{
		"summary": "a talk about sharding",
		"notes":   []interface{}{"shard by tenant"},
	}, dateHeader, topic); err != nil {
		t.Fatalf("first ReplaceTopicAnalysis failed: %v", err)
	}

	if err := SaveRawEntry("notes", "second note", dateHeader, topic); err != nil {
		t.Fatalf("second SaveRawEntry failed: %v", err)
	}
	if err := ReplaceTopicAnalysis("notes", map[string]interface{}{
		"summary": "a talk about sharding and replication",
		"notes":   []interface{}{"shard by tenant", "wal shipping is async"},
	}, dateHeader, topic); err != nil {
		t.Fatalf("second ReplaceTopicAnalysis failed: %v", err)
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Contains(content, "a talk about sharding\n") {
		t.Errorf("superseded summary was not removed:\n%s", content)
	}
	if strings.Count(content, "#### Summary") != 1 || strings.Count(content, "#### Notes") != 1 {
		t.Errorf("expected one Summary and one Notes section, got:\n%s", content)
	}
	if strings.Count(content, "- shard by tenant") != 1 {
		t.Errorf("bullet duplicated across syntheses:\n%s", content)
	}
	for _, want := range []string{"first note", "second note", "a talk about sharding and replication"} {
		if !strings.Contains(content, want) {
			t.Errorf("missing %q in:\n%s", want, content)
		}
	}

	// Summary and Notes keep their configured order, above the raw notes.
	summaryIdx := strings.Index(content, "#### Summary")
	notesIdx := strings.Index(content, "#### Notes")
	rawIdx := strings.Index(content, "#### Raw Input")
	if summaryIdx >= notesIdx || notesIdx >= rawIdx {
		t.Errorf("sections out of order (summary %d, notes %d, raw %d):\n%s", summaryIdx, notesIdx, rawIdx, content)
	}
}

// Two talks on the same day stay in separate groups.
func TestTopicsAreKeptSeparate(t *testing.T) {
	setupStorageTest(t, "markdown")
	dateHeader := time.Now().Format(getDateHeaderFormat())

	if err := SaveRawEntry("notes", "note about postgres", dateHeader, "Scaling Postgres"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveRawEntry("notes", "note about rust", dateHeader, "Rust in Production"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveRawEntry("notes", "more about postgres", dateHeader, "Scaling Postgres"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	pgIdx := strings.Index(content, "### Topic: Scaling Postgres")
	rustIdx := strings.Index(content, "### Topic: Rust in Production")
	if pgIdx == -1 || rustIdx == -1 || pgIdx > rustIdx {
		t.Fatalf("expected both topics in file order:\n%s", content)
	}
	if strings.Count(content, "### Topic: Scaling Postgres") != 1 {
		t.Errorf("topic heading duplicated:\n%s", content)
	}
	// The later postgres note belongs to the postgres block, not the rust one.
	if idx := strings.Index(content, "more about postgres"); idx > rustIdx {
		t.Errorf("note landed in the wrong topic block:\n%s", content)
	}

	notes, err := GetTopicNotes("notes", dateHeader, "Rust in Production")
	if err != nil {
		t.Fatalf("GetTopicNotes failed: %v", err)
	}
	if strings.Contains(notes, "postgres") {
		t.Errorf("topic notes leaked across topics:\n%s", notes)
	}

	topics, err := ListTopics("notes", dateHeader)
	if err != nil {
		t.Fatalf("ListTopics failed: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("ListTopics = %v, want two topics", topics)
	}
}

// An untopiced entry on a day that already has topics stays above them.
func TestUngroupedEntryCoexistsWithTopics(t *testing.T) {
	setupStorageTest(t, "markdown")
	dateHeader := time.Now().Format(getDateHeaderFormat())

	if err := SaveRawEntry("notes", "loose thought", dateHeader, ""); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveRawEntry("notes", "talk note", dateHeader, "Keynote"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveRawEntry("notes", "another loose thought", dateHeader, ""); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveEntry("notes", map[string]interface{}{"summary": "a mixed day"}, dateHeader, ""); err != nil {
		t.Fatalf("SaveEntry failed: %v", err)
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	topicIdx := strings.Index(content, "### Topic: Keynote")
	if topicIdx == -1 {
		t.Fatalf("topic block missing:\n%s", content)
	}
	for _, ungrouped := range []string{"loose thought", "another loose thought", "a mixed day"} {
		idx := strings.Index(content, ungrouped)
		if idx == -1 || idx > topicIdx {
			t.Errorf("ungrouped content %q should stay above the topics:\n%s", ungrouped, content)
		}
	}
	if idx := strings.Index(content, "talk note"); idx < topicIdx {
		t.Errorf("topic note escaped its block:\n%s", content)
	}
	// The ungrouped sections live one level up from the topic's.
	if !strings.Contains(content, "### Raw Input") || !strings.Contains(content, "#### Raw Input") {
		t.Errorf("expected raw input at both nesting levels:\n%s", content)
	}
}

// Writing into an earlier day's topic must not disturb the days after it.
func TestTopicWriteDoesNotDisturbOtherDays(t *testing.T) {
	setupStorageTest(t, "markdown")
	today := time.Now()
	yesterday := today.AddDate(0, 0, -1)
	todayHeader := today.Format(getDateHeaderFormat())
	yesterdayHeader := yesterday.Format(getDateHeaderFormat())

	if err := SaveRawEntry("notes", "old talk note", yesterdayHeader, "Old Talk"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveRawEntry("notes", "today note", todayHeader, "New Talk"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveRawEntry("notes", "another old note", yesterdayHeader, "Old Talk"); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Count(content, yesterdayHeader) != 1 || strings.Count(content, todayHeader) != 1 {
		t.Errorf("date headers duplicated:\n%s", content)
	}
	todayIdx := strings.Index(content, todayHeader)
	if idx := strings.Index(content, "another old note"); idx == -1 || idx > todayIdx {
		t.Errorf("yesterday's note landed under today:\n%s", content)
	}

	topics, err := ListTopics("notes", todayHeader)
	if err != nil {
		t.Fatalf("ListTopics failed: %v", err)
	}
	if len(topics) != 1 || topics[0] != "New Talk" {
		t.Errorf("ListTopics for today = %v, want [New Talk]", topics)
	}
}

func TestTopicsOrgFormat(t *testing.T) {
	setupStorageTest(t, "org")
	dateHeader := time.Now().Format(getDateHeaderFormat())
	const topic = "Keynote"

	if err := SaveRawEntry("notes", "first note", dateHeader, topic); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := SaveRawEntry("notes", "second note", dateHeader, topic); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}
	if err := ReplaceTopicAnalysis("notes", map[string]interface{}{"summary": "the opening talk"}, dateHeader, topic); err != nil {
		t.Fatalf("ReplaceTopicAnalysis failed: %v", err)
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Count(content, "** Topic: "+topic) != 1 {
		t.Errorf("expected one org topic heading:\n%s", content)
	}
	summaryIdx := strings.Index(content, "*** Summary")
	rawIdx := strings.Index(content, "*** Raw Input")
	if summaryIdx == -1 || rawIdx == -1 || summaryIdx > rawIdx {
		t.Errorf("org topic sections missing or out of order:\n%s", content)
	}
	if !strings.Contains(content, "first note") || !strings.Contains(content, "second note") {
		t.Errorf("notes missing:\n%s", content)
	}
}

func TestPhotoAttachedToTopic(t *testing.T) {
	setupStorageTest(t, "markdown")
	now := time.Now()
	dateHeader := now.Format(getDateHeaderFormat())
	const topic = "Scaling Postgres"

	if err := SaveRawEntry("notes", "a note", dateHeader, topic); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64))
	relPath, err := SavePhoto(now, topic, png)
	if err != nil {
		t.Fatalf("SavePhoto failed: %v", err)
	}
	if !strings.HasPrefix(relPath, "images/"+now.Format("2006-01-02")+"/") || !strings.HasSuffix(relPath, ".png") {
		t.Errorf("unexpected photo path %q", relPath)
	}
	if _, err := os.Stat(filepath.FromSlash(relPath)); err != nil {
		t.Errorf("photo not written to disk: %v", err)
	}

	// A second photo in the same second must not overwrite the first.
	secondPath, err := SavePhoto(now, topic, png)
	if err != nil {
		t.Fatalf("second SavePhoto failed: %v", err)
	}
	if secondPath == relPath {
		t.Errorf("second photo reused the path %q", relPath)
	}

	if err := SavePhotoReference("notes", dateHeader, topic, photoReference(relPath, "caption")); err != nil {
		t.Fatalf("SavePhotoReference failed: %v", err)
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if !strings.Contains(content, "#### Photos") {
		t.Errorf("photos section missing:\n%s", content)
	}
	if !strings.Contains(content, "!["+"caption"+"]("+relPath+")") {
		t.Errorf("photo link missing:\n%s", content)
	}
	photosIdx := strings.Index(content, "#### Photos")
	rawIdx := strings.Index(content, "#### Raw Input")
	if photosIdx > rawIdx {
		t.Errorf("photos should sit above the raw notes:\n%s", content)
	}

	// A synthesis arriving afterwards must not drop the photo.
	if err := ReplaceTopicAnalysis("notes", map[string]interface{}{"summary": "sharding"}, dateHeader, topic); err != nil {
		t.Fatalf("ReplaceTopicAnalysis failed: %v", err)
	}
	content, err = GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if !strings.Contains(content, relPath) || !strings.Contains(content, "a note") {
		t.Errorf("synthesis dropped the photo or the notes:\n%s", content)
	}
	if idx := strings.Index(content, "#### Summary"); idx > strings.Index(content, "#### Photos") {
		t.Errorf("summary should sit above the photos:\n%s", content)
	}
}

func TestPhotoRejectsNonImage(t *testing.T) {
	setupStorageTest(t, "markdown")

	if _, err := SavePhoto(time.Now(), "Talk", []byte("#!/bin/sh\necho hi\n")); err == nil {
		t.Error("expected a non-image upload to be rejected")
	}
}

func TestOrgPhotoReference(t *testing.T) {
	setupStorageTest(t, "org")
	got := photoReference("images/2026-09-15/120000-talk.jpg", "Talk 12:00")
	want := "[[file:images/2026-09-15/120000-talk.jpg][Talk 12:00]]"
	if got != want {
		t.Errorf("photoReference = %q, want %q", got, want)
	}
}

func TestMediaFilePathRejectsTraversal(t *testing.T) {
	setupStorageTest(t, "markdown")

	if _, ok := mediaFilePath("/api/media/../journal.md"); ok {
		t.Error("expected traversal outside the images directory to be rejected")
	}
	if _, ok := mediaFilePath("/api/media/images/../../journal.md"); ok {
		t.Error("expected traversal out of the images directory to be rejected")
	}
	if _, ok := mediaFilePath("/api/media/journal.md"); ok {
		t.Error("expected non-image paths to be rejected")
	}

	got, ok := mediaFilePath("/api/media/images/2026-09-15/120000-talk.jpg")
	if !ok {
		t.Fatal("expected a path inside the images directory to be served")
	}
	if want := filepath.FromSlash("images/2026-09-15/120000-talk.jpg"); got != want {
		t.Errorf("mediaFilePath = %q, want %q", got, want)
	}
}

func TestSanitizeTopic(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Scaling Postgres  ", "Scaling Postgres"},
		{"Talk\n### Injected", "Talk ### Injected"},
		{"a\r\nb\tc", "a  b c"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := sanitizeTopic(tc.in); got != tc.want {
			t.Errorf("sanitizeTopic(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := sanitizeTopic(strings.Repeat("a", maxTopicLength+50)); len([]rune(got)) != maxTopicLength {
		t.Errorf("sanitizeTopic did not cap length, got %d runes", len([]rune(got)))
	}
}

// A topic containing a newline would otherwise be able to forge headers in the
// journal file.
func TestTopicWithNewlineCannotForgeHeaders(t *testing.T) {
	setupStorageTest(t, "markdown")
	dateHeader := time.Now().Format(getDateHeaderFormat())

	topic := sanitizeTopic("Talk\n## 1999-01-01 Fri")
	if err := SaveRawEntry("notes", "note", dateHeader, topic); err != nil {
		t.Fatalf("SaveRawEntry failed: %v", err)
	}

	content, err := GetEntries("notes")
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}
	if strings.Contains(content, "\n## 1999-01-01 Fri") {
		t.Errorf("topic forged a date header:\n%s", content)
	}
	if strings.Count(content, dateHeader) != 1 {
		t.Errorf("expected exactly one date header:\n%s", content)
	}
}
