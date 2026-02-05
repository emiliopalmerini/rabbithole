package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/epalmerini/rabbithole/internal/proto"
	"github.com/epalmerini/rabbithole/internal/rabbitmq"
)

var decoder *proto.Decoder

var protoTypesLoaded int

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

func (m model) connectCmd() tea.Cmd {
	return func() tea.Msg {
		consumer, err := rabbitmq.NewConsumer(rabbitmq.Config{
			URL:        m.config.RabbitMQURL,
			Exchange:   m.config.Exchange,
			RoutingKey: m.config.RoutingKey,
			QueueName:  m.config.QueueName,
			Durable:    m.config.Durable,
		})
		if err != nil {
			return connectionErrorMsg{err: err}
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
					RoutingKey: del.RoutingKey,
					Exchange:   del.Exchange,
					Timestamp:  del.Timestamp,
					RawBody:    del.Body,
					Headers:    headers,
				}

				// Try to decode protobuf with routing key hint
				if decoder != nil {
					decoded, err := decoder.DecodeWithHint(del.Body, del.RoutingKey)
					if err != nil {
						msg.DecodeErr = err
					} else {
						msg.Decoded = decoded
					}
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
