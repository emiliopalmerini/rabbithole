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
	done      chan struct{}
}

// NewAsyncWriter creates a new async writer with the given store and session
func NewAsyncWriter(store Store, sessionID int64) *AsyncWriter {
	w := &AsyncWriter{
		store:     store,
		sessionID: sessionID,
		ch:        make(chan *MessageRecord, defaultBufferSize),
		done:      make(chan struct{}),
	}
	w.wg.Add(1)
	go w.run()
	return w
}

// Save queues a message for persistence. Non-blocking; drops message if buffer is full.
func (w *AsyncWriter) Save(msg *MessageRecord) bool {
	msg.SessionID = w.sessionID
	select {
	case w.ch <- msg:
		return true
	default:
		// Buffer full, drop message
		return false
	}
}

func (w *AsyncWriter) run() {
	defer w.wg.Done()
	for {
		select {
		case msg, ok := <-w.ch:
			if !ok {
				return
			}
			// Best effort insert, ignore errors
			_, _ = w.store.InsertMessage(context.Background(), msg)
		case <-w.done:
			// Drain remaining messages
			for {
				select {
				case msg, ok := <-w.ch:
					if !ok {
						return
					}
					_, _ = w.store.InsertMessage(context.Background(), msg)
				default:
					return
				}
			}
		}
	}
}

// Close gracefully shuts down the writer, draining the buffer
func (w *AsyncWriter) Close() {
	close(w.done)
	close(w.ch)
	w.wg.Wait()
}
