CREATE TABLE IF NOT EXISTS events (
    event_id TEXT PRIMARY KEY, source TEXT NOT NULL, day TEXT NOT NULL,
    timestamp TEXT NOT NULL, payload TEXT NOT NULL, fingerprint TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_day ON events(day);
CREATE INDEX IF NOT EXISTS events_timestamp ON events(timestamp DESC,event_id DESC);
CREATE TABLE IF NOT EXISTS event_revisions (
    id INTEGER PRIMARY KEY, event_id TEXT, payload TEXT, replaced_at TEXT
);
CREATE TABLE IF NOT EXISTS event_category_overrides (
    event_id TEXT PRIMARY KEY,
    category TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS category_aliases (
    source TEXT PRIMARY KEY,
    target TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS reading_sessions (
    session_id TEXT PRIMARY KEY, day TEXT, payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sync_state (
    source TEXT PRIMARY KEY, last_successful_sync TEXT, cursor TEXT,
    status TEXT NOT NULL, error_message TEXT, details TEXT
);
CREATE TABLE IF NOT EXISTS journals (
    day TEXT PRIMARY KEY, payload TEXT, input_hash TEXT, status TEXT
);
CREATE TABLE IF NOT EXISTS journal_summaries (
    day TEXT PRIMARY KEY,
    summary TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS journal_digests (
    day TEXT PRIMARY KEY,
    summary TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    digest_version INTEGER NOT NULL DEFAULT 1,
    category_summaries TEXT NOT NULL,
    work_items TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS dirty_days (day TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS suggestions (
    id INTEGER PRIMARY KEY, target TEXT, reason TEXT, payload TEXT,
    created_at TEXT, UNIQUE(target, payload)
);
CREATE TABLE IF NOT EXISTS sheet_snapshots (
    target TEXT PRIMARY KEY, payload TEXT
);
CREATE TABLE IF NOT EXISTS jobs (
    name TEXT PRIMARY KEY, last_success TEXT, details TEXT
);
CREATE TABLE IF NOT EXISTS source_coverage (
    day TEXT NOT NULL, source TEXT NOT NULL, state TEXT NOT NULL,
    observed_through TEXT NOT NULL, PRIMARY KEY(day, source)
);
CREATE TABLE IF NOT EXISTS journal_versions (
    id INTEGER PRIMARY KEY,
    day TEXT NOT NULL,
    payload TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    action TEXT NOT NULL,
    replaced_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS journal_versions_day ON journal_versions(day, id);
CREATE TABLE IF NOT EXISTS journal_reviews (
    suggestion_id INTEGER PRIMARY KEY REFERENCES suggestions(id),
    decision TEXT NOT NULL CHECK (decision IN ('accepted', 'dismissed')),
    reviewed_at TEXT NOT NULL
);
