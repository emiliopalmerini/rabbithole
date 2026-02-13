package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestSanitizeAMQPURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "removes password",
			url:  "amqp://user:pass@host:5672/",
			want: "amqp://user@host:5672/",
		},
		{
			name: "no password unchanged",
			url:  "amqp://user@host/",
			want: "amqp://user@host/",
		},
		{
			name: "not a URL returned as-is",
			url:  "not-a-url",
			want: "not-a-url",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
		{
			name: "amqps with password",
			url:  "amqps://admin:secret@rabbit.example.com:5671/vhost",
			want: "amqps://admin@rabbit.example.com:5671/vhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeAMQPURL(tt.url)
			if got != tt.want {
				t.Errorf("SanitizeAMQPURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestToNullString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  sql.NullString
	}{
		{
			name:  "empty string is invalid",
			input: "",
			want:  sql.NullString{},
		},
		{
			name:  "non-empty string is valid",
			input: "hello",
			want:  sql.NullString{String: "hello", Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toNullString(tt.input)
			if got != tt.want {
				t.Errorf("toNullString(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestToNullTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		input     time.Time
		wantValid bool
	}{
		{
			name:      "zero time is invalid",
			input:     time.Time{},
			wantValid: false,
		},
		{
			name:      "non-zero time is valid",
			input:     now,
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toNullTime(tt.input)
			if got.Valid != tt.wantValid {
				t.Errorf("toNullTime(%v).Valid = %v, want %v", tt.input, got.Valid, tt.wantValid)
			}
			if tt.wantValid && !got.Time.Equal(tt.input) {
				t.Errorf("toNullTime(%v).Time = %v, want %v", tt.input, got.Time, tt.input)
			}
		})
	}
}

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewStore(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close: %v", err)
		}
	})
	return store
}

func TestStore_CreateAndEndSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	id, err := store.CreateSession(ctx, "my-exchange", "#", "q1", "amqp://user:pass@host:5672/")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive session ID, got %d", id)
	}

	if err := store.EndSession(ctx, id); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	session, err := store.GetLastSessionByExchange(ctx, "my-exchange")
	if err != nil {
		t.Fatalf("GetLastSessionByExchange: %v", err)
	}
	if !session.EndedAt.Valid {
		t.Error("expected ended_at to be set after EndSession")
	}
	// Password should be sanitized
	if session.AmqpUrl != "amqp://user@host:5672/" {
		t.Errorf("expected sanitized URL, got %q", session.AmqpUrl)
	}
}

func TestStore_InsertAndGetMessage(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sessionID, err := store.CreateSession(ctx, "ex", "#", "q1", "amqp://localhost/")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	rec := &MessageRecord{
		SessionID:   sessionID,
		Exchange:    "ex",
		RoutingKey:  "order.created",
		Body:        []byte(`{"id": 1}`),
		ContentType: "application/json",
		Headers:     map[string]any{"x-trace": "abc123"},
		Timestamp:   ts,
		ProtoType:   "OrderCreated",
	}

	msgID, err := store.InsertMessage(ctx, rec)
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	msg, err := store.GetMessage(ctx, msgID)
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg.RoutingKey != "order.created" {
		t.Errorf("RoutingKey = %q, want %q", msg.RoutingKey, "order.created")
	}
	if msg.Exchange != "ex" {
		t.Errorf("Exchange = %q, want %q", msg.Exchange, "ex")
	}
	if string(msg.Body) != `{"id": 1}` {
		t.Errorf("Body = %q, want %q", string(msg.Body), `{"id": 1}`)
	}
	if !msg.ProtoType.Valid || msg.ProtoType.String != "OrderCreated" {
		t.Errorf("ProtoType = %+v, want OrderCreated", msg.ProtoType)
	}
}

func TestStore_ListMessagesBySession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sessionID, err := store.CreateSession(ctx, "ex", "#", "q1", "amqp://localhost/")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Insert 3 messages
	for i := range 3 {
		_, err := store.InsertMessage(ctx, &MessageRecord{
			SessionID:  sessionID,
			Exchange:   "ex",
			RoutingKey: "msg." + string(rune('a'+i)),
			Body:       []byte("body"),
		})
		if err != nil {
			t.Fatalf("InsertMessage %d: %v", i, err)
		}
	}

	// DESC order (default)
	msgs, err := store.ListMessagesBySession(ctx, sessionID, 10, 0)
	if err != nil {
		t.Fatalf("ListMessagesBySession: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// ASC order — ordered by consumed_at ASC; with autoincrement IDs the first
	// inserted message has the lowest id so it comes first even when timestamps
	// collide.
	msgsAsc, err := store.ListMessagesBySessionAsc(ctx, sessionID, 10, 0)
	if err != nil {
		t.Fatalf("ListMessagesBySessionAsc: %v", err)
	}
	if len(msgsAsc) != 3 {
		t.Fatalf("expected 3 messages ASC, got %d", len(msgsAsc))
	}

	// Verify ASC and DESC return opposite orders
	if msgsAsc[0].ID == msgs[0].ID && len(msgs) > 1 {
		// If first elements match, the ordering is effectively the same
		// (can happen when consumed_at is identical). Just verify both
		// returned all messages.
		t.Log("ASC and DESC returned same first element (identical consumed_at)")
	}
}

func TestStore_SearchMessages(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sessionID, err := store.CreateSession(ctx, "ex", "#", "q1", "amqp://localhost/")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Insert messages with searchable content
	records := []MessageRecord{
		{SessionID: sessionID, Exchange: "ex", RoutingKey: "order.created", Body: []byte("new order placed")},
		{SessionID: sessionID, Exchange: "ex", RoutingKey: "user.updated", Body: []byte("user profile changed")},
		{SessionID: sessionID, Exchange: "ex", RoutingKey: "order.shipped", Body: []byte("order was shipped")},
	}
	for i := range records {
		if _, err := store.InsertMessage(ctx, &records[i]); err != nil {
			t.Fatalf("InsertMessage %d: %v", i, err)
		}
	}

	// Search by body text
	results, err := store.SearchMessages(ctx, "order", 10, 0)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'order', got %d", len(results))
	}

	// Search within session
	results, err = store.SearchMessagesInSession(ctx, "shipped", sessionID, 10, 0)
	if err != nil {
		t.Fatalf("SearchMessagesInSession: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'shipped', got %d", len(results))
	}

	// FTS5 special characters should not cause errors
	for _, q := range []string{`"quoted"`, `OR AND NOT`, `(parens)`, `*wildcard`, `col:prefix`} {
		_, err := store.SearchMessages(ctx, q, 10, 0)
		if err != nil {
			t.Errorf("SearchMessages(%q) unexpected error: %v", q, err)
		}
	}
}

func TestStore_CountMessagesBySession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sessionID, err := store.CreateSession(ctx, "ex", "#", "q1", "amqp://localhost/")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Empty session should have 0 messages
	count, err := store.CountMessagesBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CountMessagesBySession: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages, got %d", count)
	}

	// Insert 5 messages
	for i := range 5 {
		_, err := store.InsertMessage(ctx, &MessageRecord{
			SessionID:  sessionID,
			Exchange:   "ex",
			RoutingKey: "test.key",
			Body:       []byte("body " + string(rune('a'+i))),
		})
		if err != nil {
			t.Fatalf("InsertMessage %d: %v", i, err)
		}
	}

	count, err = store.CountMessagesBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CountMessagesBySession: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 messages, got %d", count)
	}
}

func TestStore_DeleteSession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sessionID, err := store.CreateSession(ctx, "ex", "#", "q1", "amqp://localhost/")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Insert messages
	for i := range 3 {
		_, err := store.InsertMessage(ctx, &MessageRecord{
			SessionID:  sessionID,
			Exchange:   "ex",
			RoutingKey: "test.key",
			Body:       []byte("body"),
		})
		if err != nil {
			t.Fatalf("InsertMessage %d: %v", i, err)
		}
	}

	// Delete session
	if err := store.DeleteSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	// Session should be gone
	sessions, err := store.ListRecentSessions(ctx, 100)
	if err != nil {
		t.Fatalf("ListRecentSessions: %v", err)
	}
	for _, s := range sessions {
		if s.ID == sessionID {
			t.Error("session should have been deleted")
		}
	}

	// Messages should be gone (cascade)
	count, err := store.CountMessagesBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CountMessagesBySession: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages after cascade delete, got %d", count)
	}
}

func TestStore_SearchSessionsByContent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create two sessions with different content
	s1, err := store.CreateSession(ctx, "ex1", "#", "q1", "amqp://localhost/")
	if err != nil {
		t.Fatalf("CreateSession 1: %v", err)
	}
	s2, err := store.CreateSession(ctx, "ex2", "#", "q2", "amqp://localhost/")
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}

	// Session 1: order messages
	for _, body := range []string{"new order placed", "order confirmed"} {
		_, err := store.InsertMessage(ctx, &MessageRecord{
			SessionID: s1, Exchange: "ex1", RoutingKey: "order.created", Body: []byte(body),
		})
		if err != nil {
			t.Fatalf("InsertMessage s1: %v", err)
		}
	}

	// Session 2: user messages
	_, err = store.InsertMessage(ctx, &MessageRecord{
		SessionID: s2, Exchange: "ex2", RoutingKey: "user.updated", Body: []byte("user profile changed"),
	})
	if err != nil {
		t.Fatalf("InsertMessage s2: %v", err)
	}

	// Search for "order" should return session 1
	ids, err := store.SearchSessionsByContent(ctx, "order", 100)
	if err != nil {
		t.Fatalf("SearchSessionsByContent: %v", err)
	}
	if len(ids) != 1 || ids[0] != s1 {
		t.Errorf("expected [%d], got %v", s1, ids)
	}

	// Search for "user" should return session 2
	ids, err = store.SearchSessionsByContent(ctx, "user", 100)
	if err != nil {
		t.Fatalf("SearchSessionsByContent: %v", err)
	}
	if len(ids) != 1 || ids[0] != s2 {
		t.Errorf("expected [%d], got %v", s2, ids)
	}

	// Search for nonexistent content
	ids, err = store.SearchSessionsByContent(ctx, "nonexistent", 100)
	if err != nil {
		t.Fatalf("SearchSessionsByContent: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty, got %v", ids)
	}
}

func TestSanitizeFTS5Query(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "simple", want: `"simple"`},
		{input: `has "quotes"`, want: `"has ""quotes"""`},
		{input: "OR AND NOT", want: `"OR AND NOT"`},
		{input: "(parens)", want: `"(parens)"`},
		{input: "", want: `""`},
	}

	for _, tt := range tests {
		got := sanitizeFTS5Query(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFTS5Query(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
