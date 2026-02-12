package tui

import "github.com/epalmerini/rabbithole/internal/proto"

type Config struct {
	RabbitMQURL string
	Exchange    string
	RoutingKey  string
	QueueName   string
	ProtoPath   string
	ShowVersion bool
	Durable     bool
	Decoder     *proto.Decoder

	// UI options
	AutoPauseOnSelect bool    // Pause when user navigates
	DefaultSplitRatio float64 // Initial split ratio (0.5 = 50/50)
	CompactMode       bool    // Show only routing key, no timestamp

	// Persistence options
	EnablePersistence bool   // Enable SQLite persistence
	DBPath            string // Custom database path (empty = default)
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig() Config {
	return Config{
		DefaultSplitRatio: 0.5,
	}
}
