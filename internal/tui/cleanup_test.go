package tui

import (
	"context"
	"testing"

	"github.com/epalmerini/rabbithole/internal/db"
)

// cleanupStore is a minimal mock for testing cleanup behavior.
type cleanupStore struct {
	msgCount       int64
	endedSession   int64
	deletedSession int64
}

func (s *cleanupStore) CountMessagesBySession(_ context.Context, sessionID int64) (int64, error) {
	return s.msgCount, nil
}
func (s *cleanupStore) EndSession(_ context.Context, sessionID int64) error {
	s.endedSession = sessionID
	return nil
}
func (s *cleanupStore) DeleteSession(_ context.Context, sessionID int64) error {
	s.deletedSession = sessionID
	return nil
}

// Unused Store interface methods
func (s *cleanupStore) CreateSession(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}
func (s *cleanupStore) ListRecentSessions(context.Context, int64) ([]db.Session, error) {
	return nil, nil
}
func (s *cleanupStore) GetLastSessionByExchange(context.Context, string) (*db.Session, error) {
	return nil, nil
}
func (s *cleanupStore) InsertMessage(context.Context, *db.MessageRecord) (int64, error) {
	return 0, nil
}
func (s *cleanupStore) GetMessage(context.Context, int64) (*db.Message, error) { return nil, nil }
func (s *cleanupStore) ListMessagesBySession(context.Context, int64, int64, int64) ([]db.Message, error) {
	return nil, nil
}
func (s *cleanupStore) ListMessagesBySessionAsc(context.Context, int64, int64, int64) ([]db.Message, error) {
	return nil, nil
}
func (s *cleanupStore) SearchMessages(context.Context, string, int64, int64) ([]db.Message, error) {
	return nil, nil
}
func (s *cleanupStore) SearchMessagesInSession(context.Context, string, int64, int64, int64) ([]db.Message, error) {
	return nil, nil
}
func (s *cleanupStore) SearchSessionsByContent(context.Context, string, int64) ([]int64, error) {
	return nil, nil
}
func (s *cleanupStore) Close() error { return nil }

func TestCleanup_DeletesEmptySession(t *testing.T) {
	store := &cleanupStore{msgCount: 0}
	m := &model{
		store:     store,
		sessionID: 42,
	}

	m.cleanup()

	if store.deletedSession != 42 {
		t.Errorf("expected session 42 to be deleted, got %d", store.deletedSession)
	}
	if store.endedSession != 0 {
		t.Error("expected EndSession not to be called for empty session")
	}
	if m.sessionID != 0 {
		t.Error("expected sessionID to be reset to 0")
	}
}

func TestCleanup_EndsNonEmptySession(t *testing.T) {
	store := &cleanupStore{msgCount: 5}
	m := &model{
		store:     store,
		sessionID: 42,
	}

	m.cleanup()

	if store.endedSession != 42 {
		t.Errorf("expected session 42 to be ended, got %d", store.endedSession)
	}
	if store.deletedSession != 0 {
		t.Error("expected DeleteSession not to be called for non-empty session")
	}
	if m.sessionID != 0 {
		t.Error("expected sessionID to be reset to 0")
	}
}
