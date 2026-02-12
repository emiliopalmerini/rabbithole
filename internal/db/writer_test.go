package db

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockStore implements Store for testing, recording inserted messages
type mockStore struct {
	messages []*MessageRecord
	mu       sync.Mutex
}

func (s *mockStore) InsertMessage(_ context.Context, msg *MessageRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	return int64(len(s.messages)), nil
}

func (s *mockStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

// Unused Store interface methods
func (s *mockStore) CreateSession(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}
func (s *mockStore) EndSession(context.Context, int64) error                      { return nil }
func (s *mockStore) ListRecentSessions(context.Context, int64) ([]Session, error) { return nil, nil }
func (s *mockStore) GetLastSessionByExchange(context.Context, string) (*Session, error) {
	return nil, nil
}
func (s *mockStore) GetMessage(context.Context, int64) (*Message, error) { return nil, nil }
func (s *mockStore) ListMessagesBySession(context.Context, int64, int64, int64) ([]Message, error) {
	return nil, nil
}
func (s *mockStore) ListMessagesBySessionAsc(context.Context, int64, int64, int64) ([]Message, error) {
	return nil, nil
}
func (s *mockStore) SearchMessages(context.Context, string, int64, int64) ([]Message, error) {
	return nil, nil
}
func (s *mockStore) SearchMessagesInSession(context.Context, string, int64, int64, int64) ([]Message, error) {
	return nil, nil
}
func (s *mockStore) Close() error { return nil }

func TestAsyncWriter_SaveAndClose(t *testing.T) {
	store := &mockStore{}
	w := NewAsyncWriter(store, 42)

	for i := range 10 {
		msg := &MessageRecord{RoutingKey: "test", Exchange: "ex"}
		if !w.Save(msg) {
			t.Fatalf("Save failed for message %d", i)
		}
	}

	w.Close()

	if got := store.count(); got != 10 {
		t.Errorf("expected 10 messages persisted, got %d", got)
	}

	// Verify session ID was set on all messages
	store.mu.Lock()
	defer store.mu.Unlock()
	for i, msg := range store.messages {
		if msg.SessionID != 42 {
			t.Errorf("message %d: expected sessionID 42, got %d", i, msg.SessionID)
		}
	}
}

func TestAsyncWriter_SaveAfterClose(t *testing.T) {
	store := &mockStore{}
	w := NewAsyncWriter(store, 1)
	w.Close()

	// Save after close should return false, not panic
	if w.Save(&MessageRecord{RoutingKey: "test"}) {
		t.Error("Save after Close should return false")
	}
}

func TestAsyncWriter_ConcurrentSaveAndClose(t *testing.T) {
	store := &mockStore{}
	w := NewAsyncWriter(store, 1)

	var wg sync.WaitGroup
	var saved atomic.Int64

	// Spawn concurrent writers
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if w.Save(&MessageRecord{RoutingKey: "test"}) {
					saved.Add(1)
				}
			}
		}()
	}

	// Close while writers are active
	time.Sleep(time.Millisecond)
	w.Close()
	wg.Wait()

	// All persisted messages should have been inserted
	persisted := store.count()
	t.Logf("saved=%d, persisted=%d (of 1000 attempted)", saved.Load(), persisted)

	if int64(persisted) > saved.Load() {
		t.Errorf("persisted (%d) > saved (%d)", persisted, saved.Load())
	}
}

func TestAsyncWriter_DropsWhenBufferFull(t *testing.T) {
	// Use a slow store to fill the buffer
	store := &slowStore{}
	w := NewAsyncWriter(store, 1)

	// Fill beyond buffer capacity (1000)
	dropped := 0
	for range 1100 {
		if !w.Save(&MessageRecord{RoutingKey: "test"}) {
			dropped++
		}
	}

	w.Close()

	if dropped == 0 {
		t.Error("expected some messages to be dropped when buffer is full")
	}
	t.Logf("dropped %d of 1100 messages", dropped)
}

// slowStore blocks on InsertMessage to simulate a slow DB
type slowStore struct{ mockStore }

func (s *slowStore) InsertMessage(ctx context.Context, msg *MessageRecord) (int64, error) {
	time.Sleep(10 * time.Millisecond)
	return s.mockStore.InsertMessage(ctx, msg)
}
