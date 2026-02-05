package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/epalmerini/rabbithole/internal/db"
	"github.com/epalmerini/rabbithole/internal/proto"
	"github.com/epalmerini/rabbithole/internal/rabbitmq"
)

var decoder *proto.Decoder

var protoTypesLoaded int

// Persistence state (package-level for access from model methods)
var (
	store       db.Store
	asyncWriter *db.AsyncWriter
	sessionID   int64
)

// Connection retry settings
const (
	maxRetries     = 5
	maxBackoff     = 30 * time.Second
	initialBackoff = 1 * time.Second
)

func Run(cfg Config) error {
	// Initialize proto decoder if path provided
	if cfg.ProtoPath != "" {
		var err error
		decoder, err = proto.NewDecoder(cfg.ProtoPath)
		if err != nil {
			return fmt.Errorf("failed to load proto files: %w", err)
		}
		protoTypesLoaded = len(decoder.ListTypes())
	}

	// Initialize persistence if enabled
	if cfg.EnablePersistence {
		var err error
		store, err = db.NewStore(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to initialize database: %w", err)
		}
		defer func() {
			if asyncWriter != nil {
				asyncWriter.Close()
			}
			if sessionID > 0 {
				store.EndSession(context.Background(), sessionID)
			}
			store.Close()
		}()
	}

	var m tea.Model

	// If exchange is specified via CLI, go directly to consumer
	// Otherwise, show the browser
	if cfg.Exchange != "" {
		m = initialModel(cfg)
	} else {
		m = newAppModel(cfg)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running program: %w", err)
	}

	return nil
}

// retryMsg is sent when a retry attempt should be made
type retryMsg struct {
	attempt int
	delay   time.Duration
}

func (m model) connectCmd() tea.Cmd {
	return m.connectWithRetry(0)
}

func (m model) connectWithRetry(attempt int) tea.Cmd {
	return func() tea.Msg {
		consumer, err := rabbitmq.NewConsumer(rabbitmq.Config{
			URL:        m.config.RabbitMQURL,
			Exchange:   m.config.Exchange,
			RoutingKey: m.config.RoutingKey,
			QueueName:  m.config.QueueName,
			Durable:    m.config.Durable,
		})
		if err != nil {
			// Check if we should retry
			if attempt < maxRetries {
				// Calculate backoff with exponential increase
				delay := initialBackoff * time.Duration(1<<attempt)
				if delay > maxBackoff {
					delay = maxBackoff
				}
				return retryMsg{attempt: attempt + 1, delay: delay}
			}
			return connectionErrorMsg{err: fmt.Errorf("failed after %d attempts: %w", maxRetries, err)}
		}

		// Create session for persistence
		if store != nil {
			ctx := context.Background()
			queueName := m.config.QueueName
			if queueName == "" {
				queueName = "(auto-generated)"
			}
			sid, err := store.CreateSession(ctx, m.config.Exchange, m.config.RoutingKey, queueName, m.config.RabbitMQURL)
			if err == nil {
				sessionID = sid
				asyncWriter = db.NewAsyncWriter(store, sessionID)
			}
		}

		msgChan := make(chan Message, 100)

		go func() {
			ctx := context.Background()
			deliveries, err := consumer.Consume(ctx)
			if err != nil {
				return
			}

			for del := range deliveries {
				headers := make(map[string]any)
				for k, v := range del.Headers {
					headers[k] = v
				}

				msg := Message{
					RoutingKey:    del.RoutingKey,
					Exchange:      del.Exchange,
					Timestamp:     del.Timestamp,
					RawBody:       del.Body,
					Headers:       headers,
					ContentType:   del.ContentType,
					CorrelationID: del.CorrelationID,
					ReplyTo:       del.ReplyTo,
					MessageID:     del.MessageID,
					AppID:         del.AppID,
				}

				// Try to decode protobuf with routing key hint
				if decoder != nil {
					decoded, protoType, err := decoder.DecodeWithHintAndType(del.Body, del.RoutingKey)
					if err != nil {
						msg.DecodeErr = err
					} else {
						msg.Decoded = decoded
						msg.ProtoType = protoType
					}
				}

				// Persist message if enabled
				if asyncWriter != nil {
					asyncWriter.Save(&db.MessageRecord{
						Exchange:      del.Exchange,
						RoutingKey:    del.RoutingKey,
						Body:          del.Body,
						ContentType:   del.ContentType,
						Headers:       del.Headers,
						Timestamp:     del.Timestamp,
						ProtoType:     msg.ProtoType,
						CorrelationID: del.CorrelationID,
						ReplyTo:       del.ReplyTo,
						MessageID:     del.MessageID,
						AppID:         del.AppID,
					})
				}

				msgChan <- msg
			}
		}()

		return connectedMsg{msgChan: msgChan}
	}
}

func (m model) waitForMessage() tea.Cmd {
	return func() tea.Msg {
		if m.msgChan == nil {
			return nil
		}
		msg, ok := <-m.msgChan
		if !ok {
			return nil
		}
		return msgReceived{msg: msg}
	}
}

// scheduleRetry returns a command that waits and then triggers a retry
func scheduleRetry(attempt int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		return retryMsg{attempt: attempt, delay: delay}
	})
}
