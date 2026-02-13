package tui

import "github.com/epalmerini/rabbithole/internal/proto"

const defaultMaxMessages = 1000

// Config is the runtime configuration for TUI views.
type Config struct {
	RabbitMQURL   string
	ManagementURL string
	ProtoPath     string
	DBPath        string
	Decoder       *proto.Decoder
	MaxMessages   int

	// UI
	AutoPauseOnSelect bool
	DefaultSplitRatio float64
	CompactMode       bool

	// Runtime (set by browser on consume)
	Exchange   string
	RoutingKey string
	QueueName  string
	Durable    bool

	// For saving prefs back to config.toml
	ConfigDir string
}

// MessageLimit returns MaxMessages, falling back to the default if unset.
func (c Config) MessageLimit() int {
	if c.MaxMessages <= 0 {
		return defaultMaxMessages
	}
	return c.MaxMessages
}
