package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/epalmerini/rabbithole/internal/randutil"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Config struct {
	URL        string
	Exchange   string
	RoutingKey string
	QueueName  string
	Durable    bool // Create a persistent queue
}

type Delivery struct {
	RoutingKey    string
	Exchange      string
	Timestamp     time.Time
	Body          []byte
	Headers       map[string]any
	ContentType   string
	CorrelationID string
	ReplyTo       string
	MessageID     string
	AppID         string
}

type Consumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	config  Config
}

func NewConsumer(cfg Config) (*Consumer, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("failed to open channel: %w", err), conn.Close())
	}

	return &Consumer{
		conn:    conn,
		channel: ch,
		config:  cfg,
	}, nil
}

func (c *Consumer) Consume(ctx context.Context) (<-chan Delivery, error) {
	queueName := c.config.QueueName
	exclusive := false
	autoDelete := false
	durable := c.config.Durable

	// If no queue name, create an exclusive auto-delete queue
	if queueName == "" {
		queueName = fmt.Sprintf("rabbithole-%s", randutil.RandomSuffix())
		exclusive = true
		autoDelete = true
		durable = false // Auto-generated queues are never durable
	}

	var q amqp.Queue
	var err error

	// First try passive declare to check if queue exists
	q, err = c.channel.QueueDeclarePassive(
		queueName,
		false, // durable (ignored for passive)
		false, // auto-delete (ignored for passive)
		false, // exclusive (ignored for passive)
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		// Queue doesn't exist, need to recreate channel (passive declare closes it on error)
		var chanErr error
		c.channel, chanErr = c.conn.Channel()
		if chanErr != nil {
			return nil, fmt.Errorf("failed to reopen channel: %w", chanErr)
		}

		// Now declare the queue
		q, err = c.channel.QueueDeclare(
			queueName,
			durable,    // durable
			autoDelete, // auto-delete
			exclusive,  // exclusive
			false,      // no-wait
			nil,        // args
		)
		if err != nil {
			return nil, fmt.Errorf("failed to declare queue: %w", err)
		}
	}

	// Bind to exchange if specified
	if c.config.Exchange != "" {
		err = c.channel.QueueBind(
			q.Name,
			c.config.RoutingKey,
			c.config.Exchange,
			false, // no-wait
			nil,   // args
		)
		if err != nil {
			return nil, fmt.Errorf("failed to bind queue: %w", err)
		}
	}

	// Start consuming
	msgs, err := c.channel.Consume(
		q.Name,
		"",    // consumer tag
		true,  // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start consuming: %w", err)
	}

	deliveries := make(chan Delivery, 100)

	go func() {
		defer close(deliveries)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}

				ts := msg.Timestamp
				if ts.IsZero() {
					ts = time.Now()
				}

				headers := make(map[string]any)
				for k, v := range msg.Headers {
					headers[k] = v
				}

				deliveries <- Delivery{
					RoutingKey:    msg.RoutingKey,
					Exchange:      msg.Exchange,
					Timestamp:     ts,
					Body:          msg.Body,
					Headers:       headers,
					ContentType:   msg.ContentType,
					CorrelationID: msg.CorrelationId,
					ReplyTo:       msg.ReplyTo,
					MessageID:     msg.MessageId,
					AppID:         msg.AppId,
				}
			}
		}
	}()

	return deliveries, nil
}

func (c *Consumer) Close() error {
	var chanErr error
	if c.channel != nil {
		chanErr = c.channel.Close()
	}
	if c.conn != nil {
		return errors.Join(chanErr, c.conn.Close())
	}
	return chanErr
}
