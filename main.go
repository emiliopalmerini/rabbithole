package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/epalmerini/rabbithole/internal/tui"
)

var version = "dev"

func main() {
	cfg := parseFlags()

	if err := tui.Run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() tui.Config {
	cfg := tui.Config{}

	flag.StringVar(&cfg.RabbitMQURL, "url", "amqp://guest:guest@localhost:5672/", "RabbitMQ connection URL")
	flag.StringVar(&cfg.Exchange, "exchange", "", "Exchange to bind to")
	flag.StringVar(&cfg.RoutingKey, "routing-key", "#", "Routing key pattern")
	flag.StringVar(&cfg.QueueName, "queue", "", "Queue name (empty for exclusive auto-delete queue)")
	flag.StringVar(&cfg.ProtoPath, "proto", "", "Path to directory containing .proto files")
	flag.BoolVar(&cfg.ShowVersion, "version", false, "Show version")
	flag.BoolVar(&cfg.EnablePersistence, "persist", false, "Enable SQLite message persistence")
	flag.StringVar(&cfg.DBPath, "db", "", "Custom database path (default: ~/.local/share/rabbithole/rabbithole.db)")
	flag.Parse()

	if cfg.ShowVersion {
		fmt.Printf("rabbithole %s\n", version)
		os.Exit(0)
	}

	return cfg
}
