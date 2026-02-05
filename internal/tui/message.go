package tui

import "time"

// Message represents a consumed RabbitMQ message
type Message struct {
	ID         int
	RoutingKey string
	Exchange   string
	Timestamp  time.Time
	RawBody    []byte
	Decoded    map[string]any
	DecodeErr  error
	Headers    map[string]any
}
