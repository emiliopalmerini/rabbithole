package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store defines the interface for message persistence
type Store interface {
	CreateSession(ctx context.Context, exchange, routingKey, queueName, amqpURL string) (int64, error)
	EndSession(ctx context.Context, sessionID int64) error
	ListRecentSessions(ctx context.Context, limit int64) ([]Session, error)
	GetLastSessionByExchange(ctx context.Context, exchange string) (*Session, error)
	InsertMessage(ctx context.Context, msg *MessageRecord) (int64, error)
	GetMessage(ctx context.Context, id int64) (*Message, error)
	ListMessagesBySession(ctx context.Context, sessionID, limit, offset int64) ([]Message, error)
	ListMessagesBySessionAsc(ctx context.Context, sessionID, limit, offset int64) ([]Message, error)
	SearchMessages(ctx context.Context, query string, limit, offset int64) ([]Message, error)
	SearchMessagesInSession(ctx context.Context, query string, sessionID, limit, offset int64) ([]Message, error)
	Close() error
}

// MessageRecord represents a message to be inserted
type MessageRecord struct {
	SessionID     int64
	Exchange      string
	RoutingKey    string
	Body          []byte
	ContentType   string
	Headers       map[string]any
	Timestamp     time.Time
	ProtoType     string
	CorrelationID string
	ReplyTo       string
	MessageID     string
	AppID         string
}

// SQLiteStore implements Store using SQLite
type SQLiteStore struct {
	db      *sql.DB
	queries *Queries
}

// NewStore creates a new SQLite store at the default or custom path
func NewStore(customPath string) (*SQLiteStore, error) {
	dbPath := customPath
	if dbPath == "" {
		dataDir, err := defaultDataDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get data directory: %w", err)
		}
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create data directory: %w", err)
		}
		dbPath = filepath.Join(dataDir, "rabbithole.db")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys and WAL mode for better performance
	if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set pragmas: %w", err)
	}

	// Initialize schema
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &SQLiteStore{
		db:      db,
		queries: New(db),
	}, nil
}

func defaultDataDir() (string, error) {
	// Use XDG_DATA_HOME or fall back to ~/.local/share
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "rabbithole"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "rabbithole"), nil
}

func initSchema(db *sql.DB) error {
	schema := `
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
`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Create FTS5 table separately (may already exist)
	fts := `
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    body_text, routing_key, content=messages, content_rowid=id
);
`
	if _, err := db.Exec(fts); err != nil {
		return err
	}

	// Create triggers for FTS sync
	triggers := `
CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, body_text, routing_key)
    VALUES (NEW.id, CAST(NEW.body AS TEXT), NEW.routing_key);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, body_text, routing_key)
    VALUES ('delete', OLD.id, CAST(OLD.body AS TEXT), OLD.routing_key);
END;
`
	_, _ = db.Exec(triggers) // Ignore errors if triggers already exist

	return nil
}

// SanitizeAMQPURL removes password from AMQP URL for storage
func SanitizeAMQPURL(amqpURL string) string {
	u, err := url.Parse(amqpURL)
	if err != nil {
		return amqpURL
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

func (s *SQLiteStore) CreateSession(ctx context.Context, exchange, routingKey, queueName, amqpURL string) (int64, error) {
	return s.queries.CreateSession(ctx, CreateSessionParams{
		Exchange:   exchange,
		RoutingKey: routingKey,
		QueueName:  queueName,
		AmqpUrl:    SanitizeAMQPURL(amqpURL),
	})
}

func (s *SQLiteStore) EndSession(ctx context.Context, sessionID int64) error {
	return s.queries.EndSession(ctx, sessionID)
}

func (s *SQLiteStore) ListRecentSessions(ctx context.Context, limit int64) ([]Session, error) {
	return s.queries.ListRecentSessions(ctx, limit)
}

func (s *SQLiteStore) InsertMessage(ctx context.Context, msg *MessageRecord) (int64, error) {
	var headersJSON sql.NullString
	if len(msg.Headers) > 0 {
		data, err := json.Marshal(msg.Headers)
		if err == nil {
			headersJSON = sql.NullString{String: string(data), Valid: true}
		}
	}

	return s.queries.InsertMessage(ctx, InsertMessageParams{
		SessionID:     msg.SessionID,
		Exchange:      msg.Exchange,
		RoutingKey:    msg.RoutingKey,
		Body:          msg.Body,
		ContentType:   toNullString(msg.ContentType),
		Headers:       headersJSON,
		Timestamp:     toNullTime(msg.Timestamp),
		ProtoType:     toNullString(msg.ProtoType),
		CorrelationID: toNullString(msg.CorrelationID),
		ReplyTo:       toNullString(msg.ReplyTo),
		MessageID:     toNullString(msg.MessageID),
		AppID:         toNullString(msg.AppID),
	})
}

func (s *SQLiteStore) GetMessage(ctx context.Context, id int64) (*Message, error) {
	msg, err := s.queries.GetMessage(ctx, id)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *SQLiteStore) ListMessagesBySession(ctx context.Context, sessionID, limit, offset int64) ([]Message, error) {
	return s.queries.ListMessagesBySession(ctx, ListMessagesBySessionParams{
		SessionID: sessionID,
		Limit:     limit,
		Offset:    offset,
	})
}

func (s *SQLiteStore) ListMessagesBySessionAsc(ctx context.Context, sessionID, limit, offset int64) ([]Message, error) {
	return s.queries.ListMessagesBySessionAsc(ctx, ListMessagesBySessionAscParams{
		SessionID: sessionID,
		Limit:     limit,
		Offset:    offset,
	})
}

func (s *SQLiteStore) GetLastSessionByExchange(ctx context.Context, exchange string) (*Session, error) {
	session, err := s.queries.GetLastSessionByExchange(ctx, exchange)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *SQLiteStore) SearchMessages(ctx context.Context, query string, limit, offset int64) ([]Message, error) {
	const searchQuery = `
SELECT m.id, m.session_id, m.exchange, m.routing_key, m.body, m.content_type,
       m.headers, m.timestamp, m.consumed_at, m.proto_type, m.correlation_id,
       m.reply_to, m.message_id, m.app_id
FROM messages m
JOIN messages_fts fts ON m.id = fts.rowid
WHERE messages_fts MATCH ?
ORDER BY m.consumed_at DESC
LIMIT ? OFFSET ?
`
	return s.scanMessages(ctx, searchQuery, query, limit, offset)
}

func (s *SQLiteStore) SearchMessagesInSession(ctx context.Context, query string, sessionID, limit, offset int64) ([]Message, error) {
	const searchQuery = `
SELECT m.id, m.session_id, m.exchange, m.routing_key, m.body, m.content_type,
       m.headers, m.timestamp, m.consumed_at, m.proto_type, m.correlation_id,
       m.reply_to, m.message_id, m.app_id
FROM messages m
JOIN messages_fts fts ON m.id = fts.rowid
WHERE messages_fts MATCH ? AND m.session_id = ?
ORDER BY m.consumed_at DESC
LIMIT ? OFFSET ?
`
	return s.scanMessages(ctx, searchQuery, query, sessionID, limit, offset)
}

func (s *SQLiteStore) scanMessages(ctx context.Context, query string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.Exchange, &m.RoutingKey, &m.Body, &m.ContentType,
			&m.Headers, &m.Timestamp, &m.ConsumedAt, &m.ProtoType, &m.CorrelationID,
			&m.ReplyTo, &m.MessageID, &m.AppID,
		); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func toNullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}
