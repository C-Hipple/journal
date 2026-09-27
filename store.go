package main

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"time"
)

// EntryKey says where an entry is filed: under its type, on its day (the time
// of day is ignored), and in its topic, "" for none.
type EntryKey struct {
	Type  string
	Day   time.Time
	Topic string
}

// Photo is a stored photo, ready to serve.
type Photo struct {
	Name        string // file name, e.g. 091200-keynote.jpg
	ContentType string // inferred from Name when empty
	ModTime     time.Time
	Data        []byte
}

var errPhotoNotFound = errors.New("photo not found")

// Store is where entries are kept: Markdown or Org files in the git storage
// repo (fileStore), Postgres (sqlStore), or both at once (mirroredStore).
type Store interface {
	// SaveRawEntry records a note exactly as it was typed, before any AI
	// processing, and returns the ID to give SaveAnalysis for it.
	SaveRawEntry(ctx context.Context, key EntryKey, content string) (int64, error)
	// SaveAnalysis records the AI analysis of an entry that has no topic.
	SaveAnalysis(ctx context.Context, key EntryKey, entryID int64, analysis map[string]interface{}) error
	// ReplaceTopicAnalysis records a synthesis of all of a topic's notes,
	// replacing the topic's previous one.
	ReplaceTopicAnalysis(ctx context.Context, key EntryKey, analysis map[string]interface{}) error
	// AddPhoto stores an image taken at the given time and attaches it to an
	// entry. It returns the path the photo is served at, below /api/media/.
	AddPhoto(ctx context.Context, key EntryKey, at time.Time, caption string, data []byte) (string, error)
	// GetPhoto returns the photo AddPhoto stored at relPath, or
	// errPhotoNotFound.
	GetPhoto(ctx context.Context, relPath string) (*Photo, error)
	// ListTopics returns the topics used for a type on a day, oldest first.
	ListTopics(ctx context.Context, entryType string, day time.Time) ([]string, error)
	// GetTopicNotes returns the raw notes taken under a topic so far.
	GetTopicNotes(ctx context.Context, key EntryKey) (string, error)
	// GetEntries returns every entry of a type as one Markdown or Org
	// document, oldest day first.
	GetEntries(ctx context.Context, entryType string) (string, error)
}

// openStore picks the store the environment asks for: Postgres when
// DATABASE_URL is set, kept mirrored to the git storage repo when that is
// configured too. Otherwise entries go to the git storage repo, or to local
// files without one.
func openStore(databaseURL string) (Store, error) {
	if databaseURL == "" {
		return fileStore{}, nil
	}

	db, err := openSQLStore(databaseURL)
	if err != nil {
		return nil, err
	}

	// Migrate before serving. The database may not be reachable yet while the
	// machine boots, so a failure isn't fatal: each request retries the
	// migrations until they have run.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.ready(ctx); err != nil {
		log.Printf("Database not ready, will retry on the next request: %v", err)
	}

	if gitEnabled() {
		log.Println("Storing entries in Postgres, mirrored to the git storage repo.")
		return mirroredStore{primary: db, mirror: fileStore{}}, nil
	}
	log.Println("Storing entries in Postgres.")
	return db, nil
}

// fileStore keeps each entry type in a Markdown or Org file, committed and
// pushed to the storage repo when git storage is configured. It adapts the
// functions in storage.go and media.go to Store.
type fileStore struct{}

func dateHeaderFor(day time.Time) string {
	return day.Format(getDateHeaderFormat())
}

// SaveRawEntry has no ID to return: the file store files analyses by day.
func (fileStore) SaveRawEntry(_ context.Context, key EntryKey, content string) (int64, error) {
	return 0, SaveRawEntry(key.Type, content, dateHeaderFor(key.Day), key.Topic)
}

func (fileStore) SaveAnalysis(_ context.Context, key EntryKey, _ int64, analysis map[string]interface{}) error {
	return SaveEntry(key.Type, analysis, dateHeaderFor(key.Day), key.Topic)
}

func (fileStore) ReplaceTopicAnalysis(_ context.Context, key EntryKey, analysis map[string]interface{}) error {
	return ReplaceTopicAnalysis(key.Type, analysis, dateHeaderFor(key.Day), key.Topic)
}

func (fileStore) AddPhoto(_ context.Context, key EntryKey, at time.Time, caption string, data []byte) (string, error) {
	relPath, err := SavePhoto(at, key.Topic, data)
	if err != nil {
		return "", err
	}
	if err := SavePhotoReference(key.Type, dateHeaderFor(key.Day), key.Topic, photoReference(relPath, caption)); err != nil {
		return "", err
	}
	return relPath, nil
}

func (fileStore) GetPhoto(_ context.Context, relPath string) (*Photo, error) {
	filePath, ok := photoFilePath(relPath)
	if !ok {
		return nil, errPhotoNotFound
	}

	info, err := os.Stat(filePath)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
		return nil, errPhotoNotFound
	}
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return &Photo{Name: info.Name(), ModTime: info.ModTime(), Data: data}, nil
}

func (fileStore) ListTopics(_ context.Context, entryType string, day time.Time) ([]string, error) {
	return ListTopics(entryType, dateHeaderFor(day))
}

func (fileStore) GetTopicNotes(_ context.Context, key EntryKey) (string, error) {
	return GetTopicNotes(key.Type, dateHeaderFor(key.Day), key.Topic)
}

func (fileStore) GetEntries(_ context.Context, entryType string) (string, error) {
	return GetEntries(entryType)
}

// mirroredStore reads from and writes to primary, and repeats every write on
// mirror, so the database and the git storage repo both stay up to date. Entry
// IDs are primary's, so mirror must be a store that doesn't use them, as
// fileStore doesn't.
//
// A write that fails on the mirror is logged rather than returned: the entry is
// already safe in primary, and failing the request would only get it submitted
// twice.
type mirroredStore struct {
	primary Store
	mirror  Store
}

func logMirrorError(what string, err error) {
	if err != nil {
		log.Printf("Error mirroring %s: %v", what, err)
	}
}

func (m mirroredStore) SaveRawEntry(ctx context.Context, key EntryKey, content string) (int64, error) {
	id, err := m.primary.SaveRawEntry(ctx, key, content)
	if err != nil {
		return 0, err
	}
	_, err = m.mirror.SaveRawEntry(ctx, key, content)
	logMirrorError("entry", err)
	return id, nil
}

func (m mirroredStore) SaveAnalysis(ctx context.Context, key EntryKey, entryID int64, analysis map[string]interface{}) error {
	if err := m.primary.SaveAnalysis(ctx, key, entryID, analysis); err != nil {
		return err
	}
	logMirrorError("analysis", m.mirror.SaveAnalysis(ctx, key, entryID, analysis))
	return nil
}

func (m mirroredStore) ReplaceTopicAnalysis(ctx context.Context, key EntryKey, analysis map[string]interface{}) error {
	if err := m.primary.ReplaceTopicAnalysis(ctx, key, analysis); err != nil {
		return err
	}
	logMirrorError("topic synthesis", m.mirror.ReplaceTopicAnalysis(ctx, key, analysis))
	return nil
}

func (m mirroredStore) AddPhoto(ctx context.Context, key EntryKey, at time.Time, caption string, data []byte) (string, error) {
	relPath, err := m.primary.AddPhoto(ctx, key, at, caption, data)
	if err != nil {
		return "", err
	}
	_, err = m.mirror.AddPhoto(ctx, key, at, caption, data)
	logMirrorError("photo", err)
	return relPath, nil
}

func (m mirroredStore) GetPhoto(ctx context.Context, relPath string) (*Photo, error) {
	return m.primary.GetPhoto(ctx, relPath)
}

func (m mirroredStore) ListTopics(ctx context.Context, entryType string, day time.Time) ([]string, error) {
	return m.primary.ListTopics(ctx, entryType, day)
}

func (m mirroredStore) GetTopicNotes(ctx context.Context, key EntryKey) (string, error) {
	return m.primary.GetTopicNotes(ctx, key)
}

func (m mirroredStore) GetEntries(ctx context.Context, entryType string) (string, error) {
	return m.primary.GetEntries(ctx, entryType)
}
