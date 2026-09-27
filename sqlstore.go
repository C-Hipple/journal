package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sqlStore keeps entries in Postgres, such as a Supabase project, in the
// journal schema that the files in migrations/ set up.
type sqlStore struct {
	pool *pgxpool.Pool

	migrateMu sync.Mutex
	migrated  bool
}

// openSQLStore sets up a connection pool. Nothing is dialled until the first
// query, so a database that can't be reached yet isn't an error here.
func openSQLStore(databaseURL string) (*sqlStore, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	return newSQLStore(config)
}

func newSQLStore(config *pgxpool.Config) (*sqlStore, error) {
	// While the database is down, fail requests quickly rather than hang them.
	if config.ConnConfig.ConnectTimeout == 0 {
		config.ConnConfig.ConnectTimeout = 10 * time.Second
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}
	return &sqlStore{pool: pool}, nil
}

// ready applies any pending migrations. Until that has succeeded once, every
// call tries again, since the database may not have been reachable when the app
// started.
func (s *sqlStore) ready(ctx context.Context) error {
	s.migrateMu.Lock()
	defer s.migrateMu.Unlock()

	if s.migrated {
		return nil
	}
	if err := migrate(ctx, s.pool); err != nil {
		return fmt.Errorf("migrating the database: %w", err)
	}
	s.migrated = true
	return nil
}

// sqlDate renders the day an entry is filed under for a DATE parameter. It goes
// over as text, cast in the query, so no time zone conversion on the way can
// move it to another day.
func sqlDate(day time.Time) string {
	return day.Format("2006-01-02")
}

// upsertTopic returns the ID of the key's topic, creating the topic the first
// time it's used, or nil when the key has no topic.
func upsertTopic(ctx context.Context, tx pgx.Tx, key EntryKey) (*int64, error) {
	if key.Topic == "" {
		return nil, nil
	}
	var id int64
	// The no-op update makes RETURNING give back the ID of an existing topic.
	err := tx.QueryRow(ctx, `
		INSERT INTO journal.topics (entry_type, entry_date, name)
		VALUES ($1, $2::date, $3)
		ON CONFLICT (entry_type, entry_date, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`,
		key.Type, sqlDate(key.Day), key.Topic).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *sqlStore) SaveRawEntry(ctx context.Context, key EntryKey, content string) (int64, error) {
	if err := s.ready(ctx); err != nil {
		return 0, err
	}

	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		topicID, err := upsertTopic(ctx, tx, key)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO journal.entries (entry_type, entry_date, topic_id, raw_input)
			VALUES ($1, $2::date, $3, $4)
			RETURNING id`,
			key.Type, sqlDate(key.Day), topicID, content).Scan(&id)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *sqlStore) SaveAnalysis(ctx context.Context, _ EntryKey, entryID int64, analysis map[string]interface{}) error {
	doc, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	if err := s.ready(ctx); err != nil {
		return err
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE journal.entries SET analysis = $2::jsonb, analyzed_at = now()
		WHERE id = $1`,
		entryID, string(doc))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("entry %d not found", entryID)
	}
	return nil
}

func (s *sqlStore) ReplaceTopicAnalysis(ctx context.Context, key EntryKey, analysis map[string]interface{}) error {
	if key.Topic == "" {
		return errors.New("a topic synthesis needs a topic")
	}
	doc, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	if err := s.ready(ctx); err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO journal.topics (entry_type, entry_date, name, synthesis, synthesized_at)
		VALUES ($1, $2::date, $3, $4::jsonb, now())
		ON CONFLICT (entry_type, entry_date, name)
		DO UPDATE SET synthesis = EXCLUDED.synthesis, synthesized_at = EXCLUDED.synthesized_at`,
		key.Type, sqlDate(key.Day), key.Topic, string(doc))
	return err
}

func (s *sqlStore) AddPhoto(ctx context.Context, key EntryKey, at time.Time, caption string, data []byte) (string, error) {
	contentType, ext, err := photoType(data)
	if err != nil {
		return "", err
	}
	if err := s.ready(ctx); err != nil {
		return "", err
	}

	var relPath string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		topicID, err := upsertTopic(ctx, tx, key)
		if err != nil {
			return err
		}
		// Photos that would share a path, like two taken in the same second,
		// are told apart the same way as on disk.
		for attempt := 0; ; attempt++ {
			relPath = path.Join(photoDirFor(at), photoFileName(at, key.Topic, ext, attempt))
			tag, err := tx.Exec(ctx, `
				INSERT INTO journal.photos (entry_type, entry_date, topic_id, path, caption, content_type, data)
				VALUES ($1, $2::date, $3, $4, $5, $6, $7)
				ON CONFLICT (path) DO NOTHING`,
				key.Type, sqlDate(key.Day), topicID, relPath, caption, contentType, data)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				return nil
			}
		}
	})
	if err != nil {
		return "", err
	}
	return relPath, nil
}

func (s *sqlStore) GetPhoto(ctx context.Context, relPath string) (*Photo, error) {
	relPath, ok := cleanPhotoPath(relPath)
	if !ok {
		return nil, errPhotoNotFound
	}
	if err := s.ready(ctx); err != nil {
		return nil, err
	}

	photo := Photo{Name: path.Base(relPath)}
	err := s.pool.QueryRow(ctx, `
		SELECT content_type, created_at, data FROM journal.photos WHERE path = $1`,
		relPath).Scan(&photo.ContentType, &photo.ModTime, &photo.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errPhotoNotFound
	}
	if err != nil {
		return nil, err
	}
	return &photo, nil
}

func (s *sqlStore) ListTopics(ctx context.Context, entryType string, day time.Time) ([]string, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT name FROM journal.topics
		WHERE entry_type = $1 AND entry_date = $2::date
		ORDER BY id`,
		entryType, sqlDate(day))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (s *sqlStore) GetTopicNotes(ctx context.Context, key EntryKey) (string, error) {
	if err := s.ready(ctx); err != nil {
		return "", err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.raw_input
		FROM journal.entries e
		JOIN journal.topics t ON t.id = e.topic_id
		WHERE t.entry_type = $1 AND t.entry_date = $2::date AND t.name = $3
		ORDER BY e.id`,
		key.Type, sqlDate(key.Day), key.Topic)
	if err != nil {
		return "", err
	}
	notes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}

	// Joined the way the file store's Raw Input section joins them.
	for i, note := range notes {
		notes[i] = strings.TrimRight(note, "\n")
	}
	return strings.TrimSpace(strings.Join(notes, "\n")), nil
}

// GetEntries lays the entries out as the document the file store would have
// written, so the frontend reads either one the same way.
func (s *sqlStore) GetEntries(ctx context.Context, entryType string) (string, error) {
	if err := s.ready(ctx); err != nil {
		return "", err
	}

	var days []*journalDay
	// One snapshot, so a note saved in the meantime can't turn up without its
	// topic.
	txOptions := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	err := pgx.BeginTxFunc(ctx, s.pool, txOptions, func(tx pgx.Tx) error {
		var err error
		days, err = loadJournal(ctx, tx, entryType)
		return err
	})
	if err != nil {
		return "", err
	}
	return renderJournal(entryType, days), nil
}

// journalDay is one day of entries of a type: those filed directly under the
// day, then each topic in the order it was started.
type journalDay struct {
	date      time.Time
	ungrouped entryGroup
	topics    []*entryGroup
}

// entryGroup is what one block of a day holds, each part in the order it was
// saved.
type entryGroup struct {
	topic    string
	analyses []map[string]interface{}
	photos   []string // rendered links
	notes    []string
}

type topicRow struct {
	ID        int64
	Date      time.Time
	Name      string
	Synthesis map[string]interface{}
}

type entryRow struct {
	Date     time.Time
	TopicID  *int64
	RawInput string
	Analysis map[string]interface{}
}

type photoRow struct {
	Date    time.Time
	TopicID *int64
	Path    string
	Caption string
}

// queryRows runs a query and scans each row, column by column, into a T.
func queryRows[T any](ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[T])
}

// loadJournal reads every entry of a type, grouped by day and topic, oldest day
// first.
func loadJournal(ctx context.Context, tx pgx.Tx, entryType string) ([]*journalDay, error) {
	topics, err := queryRows[topicRow](ctx, tx, `
		SELECT id, entry_date, name, synthesis FROM journal.topics
		WHERE entry_type = $1 ORDER BY id`, entryType)
	if err != nil {
		return nil, err
	}
	entries, err := queryRows[entryRow](ctx, tx, `
		SELECT entry_date, topic_id, raw_input, analysis FROM journal.entries
		WHERE entry_type = $1 ORDER BY id`, entryType)
	if err != nil {
		return nil, err
	}
	photos, err := queryRows[photoRow](ctx, tx, `
		SELECT entry_date, topic_id, path, caption FROM journal.photos
		WHERE entry_type = $1 ORDER BY id`, entryType)
	if err != nil {
		return nil, err
	}

	days := map[string]*journalDay{}
	dayOf := func(date time.Time) *journalDay {
		day, ok := days[sqlDate(date)]
		if !ok {
			day = &journalDay{date: date}
			days[sqlDate(date)] = day
		}
		return day
	}

	groups := map[int64]*entryGroup{}
	for _, topic := range topics {
		group := &entryGroup{topic: topic.Name}
		if topic.Synthesis != nil {
			group.analyses = append(group.analyses, topic.Synthesis)
		}
		day := dayOf(topic.Date)
		day.topics = append(day.topics, group)
		groups[topic.ID] = group
	}
	groupOf := func(date time.Time, topicID *int64) *entryGroup {
		if topicID != nil {
			if group, ok := groups[*topicID]; ok {
				return group
			}
		}
		return &dayOf(date).ungrouped
	}

	for _, entry := range entries {
		group := groupOf(entry.Date, entry.TopicID)
		group.notes = append(group.notes, entry.RawInput)
		if entry.Analysis != nil {
			group.analyses = append(group.analyses, entry.Analysis)
		}
	}
	for _, photo := range photos {
		group := groupOf(photo.Date, photo.TopicID)
		group.photos = append(group.photos, photoReference(photo.Path, photo.Caption))
	}

	ordered := make([]*journalDay, 0, len(days))
	for _, date := range slices.Sorted(maps.Keys(days)) {
		ordered = append(ordered, days[date])
	}
	return ordered, nil
}

// renderJournal writes days out as the file store lays them out, using the same
// helpers.
func renderJournal(entryType string, days []*journalDay) string {
	if len(days) == 0 {
		return ""
	}

	blocks := make([]string, len(days))
	for i, day := range days {
		blocks[i] = strings.TrimRight(renderDay(entryType, day), "\n")
	}
	// Separated as ensureTrailingBlank separates them.
	separator := "\n"
	if journalFormat == "markdown" {
		separator = "\n\n"
	}
	return strings.Join(blocks, separator) + "\n"
}

func renderDay(entryType string, day *journalDay) string {
	header := dateHeaderFor(day.date)
	text := applyToEntry("", header, "", day.ungrouped.fill(entryType))
	for _, topic := range day.topics {
		text = applyToEntry(text, header, topic.topic, topic.fill(entryType))
	}
	return text
}

// fill returns an applyToEntry mutator that writes the group into its block.
// The raw notes go in first, as they are saved first, so that photos and then
// analyses are inserted above them just as when they're saved to a file.
func (g *entryGroup) fill(entryType string) func(block string, level sectionLevel) string {
	return func(block string, level sectionLevel) string {
		for _, note := range g.notes {
			block = appendToSection(block, level.prefix+"Raw Input", note, level)
		}
		for _, photo := range g.photos {
			block = appendToSection(block, level.prefix+"Photos", photo, level)
		}
		for _, analysis := range g.analyses {
			block = applyAnalysis(entryType, block, analysis, level, false)
		}
		return block
	}
}
