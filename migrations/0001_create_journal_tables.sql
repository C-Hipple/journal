-- Tables for the SQL storage backend.
--
-- They live in their own schema rather than public, which Supabase exposes
-- through its auto-generated REST API. Row level security is enabled with no
-- policies as a second line of defence: only the role that owns the tables,
-- the one the app connects and migrates as, can read or write them.

CREATE SCHEMA IF NOT EXISTS journal;

-- A topic groups a day's notes on one subject, such as a conference talk.
CREATE TABLE journal.topics (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entry_type     TEXT NOT NULL,
    entry_date     DATE NOT NULL,
    name           TEXT NOT NULL,
    -- AI synthesis of every note taken under the topic, rewritten as each new
    -- note arrives.
    synthesis      JSONB,
    synthesized_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entry_type, entry_date, name)
);

CREATE TABLE journal.entries (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entry_type  TEXT NOT NULL,
    entry_date  DATE NOT NULL,
    topic_id    BIGINT REFERENCES journal.topics (id),
    -- The note exactly as it was typed, saved before any AI processing.
    raw_input   TEXT NOT NULL,
    -- AI analysis of this entry alone. Notes under a topic are analysed
    -- together instead, in topics.synthesis.
    analysis    JSONB,
    analyzed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX entries_entry_type_entry_date_idx ON journal.entries (entry_type, entry_date);
CREATE INDEX entries_topic_id_idx ON journal.entries (topic_id);

CREATE TABLE journal.photos (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    entry_type   TEXT NOT NULL,
    entry_date   DATE NOT NULL,
    topic_id     BIGINT REFERENCES journal.topics (id),
    -- Served at /api/media/<path>, e.g. images/2025-01-15/091200-keynote.jpg
    path         TEXT NOT NULL UNIQUE,
    caption      TEXT NOT NULL,
    content_type TEXT NOT NULL,
    data         BYTEA NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX photos_entry_type_entry_date_idx ON journal.photos (entry_type, entry_date);

ALTER TABLE journal.topics ENABLE ROW LEVEL SECURITY;
ALTER TABLE journal.entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE journal.photos ENABLE ROW LEVEL SECURITY;
