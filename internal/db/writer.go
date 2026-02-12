package db

import (
	"context"
	"sync"
)

const defaultBufferSize = 1000

// AsyncWriter provides non-blocking message persistence with a buffered channel
type AsyncWriter struct {
	store     Store
	sessionID int64
	ch        chan *MessageRecord
	wg        sync.WaitGroup
	mu        sync.RWMutex
	closed    bool
}

// NewAsyncWriter creates a new async writer with the given store and session
func NewAsyncWriter(store Store, sessionID int64) *AsyncWriter {
	w := &AsyncWriter{
		store:     store,
		sessionID: sessionID,
		ch:        make(chan *MessageRecord, defaultBufferSize),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// Save queues a message for persistence. Non-blocking; drops message if buffer is full.
// Returns false if the writer is closed or the buffer is full.
func (w *AsyncWriter) Save(msg *MessageRecord) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return false
	}
	msg.SessionID = w.sessionID
	select {
	case w.ch <- msg:
		return true
	default:
		return false
	}
}

func (w *AsyncWriter) run() {
	defer w.wg.Done()
	for msg := range w.ch {
		// Best effort insert, ignore errors
		_, _ = w.store.InsertMessage(context.Background(), msg)
	}
}

// Close gracefully shuts down the writer, draining the buffer
func (w *AsyncWriter) Close() {
	w.mu.Lock()
	w.closed = true
	close(w.ch)
	w.mu.Unlock()
	w.wg.Wait()
}
