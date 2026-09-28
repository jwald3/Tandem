package store

// schema creates every table and index. It's idempotent (IF NOT EXISTS), so it
// runs on every start; changes to existing tables go in a migrate* step below.
//
// Reminders, interactions and notes each optionally belong to a contact.
// Deleting a contact unlinks them (ON DELETE SET NULL) rather than deleting
// them, so no history is lost.
//
// User-data timestamps are local time, like every date the app shows; chat
// timestamps stay UTC (they are only used for ordering).
const schema = `
CREATE TABLE IF NOT EXISTS contacts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    relationship TEXT NOT NULL DEFAULT '', -- coworker, friend, family, client...
    company      TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '', -- job title / role
    email        TEXT NOT NULL DEFAULT '',
    phone        TEXT NOT NULL DEFAULT '',
    birthday     TEXT NOT NULL DEFAULT '', -- YYYY-MM-DD or --MM-DD when the year is unknown
    about        TEXT NOT NULL DEFAULT '', -- short free-form summary
    created_at   TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts(name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS reminders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT NOT NULL,
    notes      TEXT NOT NULL DEFAULT '',
    due_date   TEXT NOT NULL DEFAULT '', -- YYYY-MM-DD, '' = no date
    due_time   TEXT NOT NULL DEFAULT '', -- HH:MM (24h), '' = all day
    contact_id INTEGER REFERENCES contacts(id) ON DELETE SET NULL,
    done_at    TEXT,                     -- NULL while open
    created_at TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX IF NOT EXISTS idx_reminders_due ON reminders(done_at, due_date);
CREATE INDEX IF NOT EXISTS idx_reminders_contact ON reminders(contact_id);

CREATE TABLE IF NOT EXISTS interactions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id INTEGER REFERENCES contacts(id) ON DELETE SET NULL,
    kind       TEXT NOT NULL DEFAULT 'other', -- meeting, call, email, message, other
    date       TEXT NOT NULL,
    summary    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX IF NOT EXISTS idx_interactions_date ON interactions(date);
CREATE INDEX IF NOT EXISTS idx_interactions_contact ON interactions(contact_id);

CREATE TABLE IF NOT EXISTS notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id INTEGER REFERENCES contacts(id) ON DELETE SET NULL,
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now', 'localtime'))
);
CREATE INDEX IF NOT EXISTS idx_notes_contact ON notes(contact_id);

CREATE TABLE IF NOT EXISTS chat_threads (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT NOT NULL DEFAULT 'New chat',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS chat_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id  INTEGER REFERENCES chat_threads(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    content    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'done', -- 'pending' | 'done' | 'error'
    mutated    INTEGER NOT NULL DEFAULT 0,    -- assistant reply changed the user's data
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_chat_thread ON chat_messages(thread_id);
CREATE TABLE IF NOT EXISTS chat_images (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES chat_messages(id) ON DELETE CASCADE,
    media_type TEXT NOT NULL,
    data       BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_chat_images_message ON chat_images(message_id);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// A reply left 'pending' by a crash mid-generation can never complete;
	// mark such orphans as errored so the UI stops waiting on them.
	_, err := s.db.Exec(`UPDATE chat_messages SET status = 'error', content = 'This reply was interrupted. Please ask again.' WHERE status = 'pending'`)
	return err
}
