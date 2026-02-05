CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    exchange TEXT NOT NULL,
    routing_key TEXT NOT NULL,
    queue_name TEXT NOT NULL,
    amqp_url TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    exchange TEXT NOT NULL,
    routing_key TEXT NOT NULL,
    body BLOB NOT NULL,
    content_type TEXT,
    headers TEXT,
    timestamp DATETIME,
    consumed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    proto_type TEXT,
    correlation_id TEXT,
    reply_to TEXT,
    message_id TEXT,
    app_id TEXT
);

CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_routing_key ON messages(routing_key);

-- FTS5 for full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    body_text, routing_key, content=messages, content_rowid=id
);

-- Triggers to keep FTS in sync
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, body_text, routing_key)
    VALUES (NEW.id, CAST(NEW.body AS TEXT), NEW.routing_key);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, body_text, routing_key)
    VALUES ('delete', OLD.id, CAST(OLD.body AS TEXT), OLD.routing_key);
END;
