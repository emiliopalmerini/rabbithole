package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/epalmerini/rabbithole/internal/db"
	"github.com/epalmerini/rabbithole/internal/proto"
	"github.com/epalmerini/rabbithole/internal/rabbitmq"
)

// Connection retry settings
const (
	maxRetries     = 5
	maxBackoff     = 30 * time.Second
	initialBackoff = 1 * time.Second
)

func Run(cfg Config) error {
	// Initialize persistence store if enabled (shared across consumer sessions)
	var persistStore db.Store
	if cfg.EnablePersistence {
		var err error
		persistStore, err = db.NewStore(cfg.DBPath)
		if err != nil {
			return fmt.Errorf("failed to initialize database: %w", err)
		}
		defer func() { _ = persistStore.Close() }()
	}

	var m tea.Model

	// If exchange is specified via CLI, go directly to consumer
	// Otherwise, show the browser
	if cfg.Exchange != "" {
		m = initialModel(cfg, persistStore)
	} else {
		m = newAppModel(cfg, persistStore)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("error running program: %w", err)
	}

	// Clean up consumer resources from final model state
	switch fm := finalModel.(type) {
	case model:
		fm.cleanup()
	case appModel:
		fm.consumer.cleanup()
	}

	return nil
}

// connectionLostMsg is sent when the consumer channel closes unexpectedly during consumption
type connectionLostMsg struct{}

// retryMsg is sent when a connection attempt fails and should be retried after a delay
type retryMsg struct {
	attempt int
	delay   time.Duration
}

// retryTickMsg is sent after the backoff delay has elapsed, triggering the actual retry
type retryTickMsg struct {
	attempt int
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

		// Clean up previous consumer resources (captured from model snapshot)
		m.cleanup()

		// Load historical messages and create session for persistence
		var historicalMsgs []Message
		var writer *db.AsyncWriter
		var sid int64

		if m.store != nil {
			ctx := context.Background()

			// Load messages from last session on this exchange
			lastSession, err := m.store.GetLastSessionByExchange(ctx, m.config.Exchange)
			if err == nil && lastSession != nil {
				dbMsgs, err := m.store.ListMessagesBySessionAsc(ctx, lastSession.ID, int64(m.config.MessageLimit()), 0)
				if err == nil {
					historicalMsgs = convertDBMessages(dbMsgs, m.config.Decoder)
				}
			}

			queueName := m.config.QueueName
			if queueName == "" {
				queueName = "(auto-generated)"
			}
			newSID, err := m.store.CreateSession(ctx, m.config.Exchange, m.config.RoutingKey, queueName, m.config.RabbitMQURL)
			if err == nil {
				sid = newSID
				writer = db.NewAsyncWriter(m.store, sid)
			}
		}

		// Create cancellable context for the consumer goroutine
		ctx, cancel := context.WithCancel(context.Background())

		msgChan := make(chan Message, 100)

		go func() {
			defer close(msgChan)
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
				if m.config.Decoder != nil {
					decoded, protoType, err := m.config.Decoder.DecodeWithHintAndType(del.Body, del.RoutingKey)
					if err != nil {
						msg.DecodeErr = err
					} else {
						msg.Decoded = decoded
						msg.ProtoType = protoType
					}
				}

				// Persist message if enabled (uses local writer, not global)
				if writer != nil {
					writer.Save(&db.MessageRecord{
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

		return connectedMsg{
			msgChan:         msgChan,
			historicalMsgs:  historicalMsgs,
			historicalCount: len(historicalMsgs),
			consumer:        consumer,
			cancelConsume:   cancel,
			asyncWriter:     writer,
			sessionID:       sid,
		}
	}
}

func (m model) waitForMessage() tea.Cmd {
	return func() tea.Msg {
		if m.msgChan == nil {
			return nil
		}
		msg, ok := <-m.msgChan
		if !ok {
			return connectionLostMsg{}
		}
		return msgReceived{msg: msg}
	}
}

// scheduleRetry returns a command that waits for the backoff delay, then triggers the retry
func scheduleRetry(attempt int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(_ time.Time) tea.Msg {
		return retryTickMsg{attempt: attempt}
	})
}

// cleanup releases consumer and persistence resources.
// Safe to call on zero-value or already-cleaned-up models.
func (m *model) cleanup() {
	if m.cancelConsume != nil {
		m.cancelConsume()
		m.cancelConsume = nil
	}
	if m.amqpConsumer != nil {
		_ = m.amqpConsumer.Close()
		m.amqpConsumer = nil
	}
	if m.asyncWriter != nil {
		m.asyncWriter.Close()
		m.asyncWriter = nil
	}
	if m.sessionID > 0 && m.store != nil {
		_ = m.store.EndSession(context.Background(), m.sessionID)
		m.sessionID = 0
	}
}

// initialReplayModel creates a consumer model pre-loaded with session messages (no AMQP).
func initialReplayModel(cfg Config, session db.Session, dbMsgs []db.Message) model {
	si := textinput.New()
	si.Placeholder = "Search..."
	si.CharLimit = 100
	si.Width = 30

	fi := textinput.New()
	fi.Placeholder = "Filter..."
	fi.CharLimit = 100
	fi.Width = 30

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle

	splitRatio := loadSplitRatio(cfg.DefaultSplitRatio)

	msgs := convertDBMessages(dbMsgs, cfg.Decoder)

	return model{
		config:         cfg,
		replayMode:     true,
		messages:       msgs,
		messageCount:   len(msgs),
		connState:      stateConnected,
		viewport:       viewport.New(80, 20),
		detailViewport: viewport.New(80, 20),
		vimKeys:        NewVimKeyState(),
		bookmarks:      make(map[int]bool),
		splitRatio:     splitRatio,
		compactMode:    cfg.CompactMode,
		searchInput:    si,
		filterInput:    fi,
		spinner:        sp,
	}
}

// convertDBMessages converts database messages to TUI messages
func convertDBMessages(dbMsgs []db.Message, dec *proto.Decoder) []Message {
	msgs := make([]Message, len(dbMsgs))
	for i, dbMsg := range dbMsgs {
		msg := Message{
			ID:         i + 1,
			Exchange:   dbMsg.Exchange,
			RoutingKey: dbMsg.RoutingKey,
			RawBody:    dbMsg.Body,
			Historical: true,
		}
		if dbMsg.Timestamp.Valid {
			msg.Timestamp = dbMsg.Timestamp.Time
		}
		if dbMsg.ContentType.Valid {
			msg.ContentType = dbMsg.ContentType.String
		}
		if dbMsg.ProtoType.Valid {
			msg.ProtoType = dbMsg.ProtoType.String
		}
		if dbMsg.CorrelationID.Valid {
			msg.CorrelationID = dbMsg.CorrelationID.String
		}
		if dbMsg.ReplyTo.Valid {
			msg.ReplyTo = dbMsg.ReplyTo.String
		}
		if dbMsg.MessageID.Valid {
			msg.MessageID = dbMsg.MessageID.String
		}
		if dbMsg.AppID.Valid {
			msg.AppID = dbMsg.AppID.String
		}
		if dbMsg.Headers.Valid {
			var headers map[string]any
			if err := json.Unmarshal([]byte(dbMsg.Headers.String), &headers); err == nil {
				msg.Headers = headers
			}
		}
		// Try to decode protobuf if decoder is available
		if dec != nil {
			decoded, protoType, err := dec.DecodeWithHintAndType(dbMsg.Body, dbMsg.RoutingKey)
			if err == nil {
				msg.Decoded = decoded
				msg.ProtoType = protoType
			}
		}
		msgs[i] = msg
	}
	return msgs
}
