# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
# Build
go build -o rabbithole .

# Run (browser mode - shows topology explorer)
./rabbithole -url "amqp://user:pass@host:5672/"

# Run (direct consumer mode - bypasses browser)
./rabbithole -exchange myexchange -routing-key "#"

# Run with protobuf decoding
./rabbithole -exchange myexchange -proto /path/to/proto/files

# Install globally
go install github.com/epalmerini/rabbithole@latest
```

## Architecture

This is a TUI application for consuming and inspecting RabbitMQ messages, built with the Bubble Tea framework.

### Package Structure

- `main.go` - Entry point, CLI flag parsing
- `internal/tui/` - TUI layer using Bubble Tea (Elm architecture: Model-Update-View)
- `internal/rabbitmq/` - RabbitMQ integration (AMQP consumer + Management API client)
- `internal/proto/` - Dynamic protobuf decoding using protoreflect

### TUI Architecture (Bubble Tea)

The app has two main views managed by `appModel`:
1. **Browser view** (`browserModel`) - Topology explorer showing exchanges/bindings, allows creating queues
2. **Consumer view** (`model`) - Real-time message consumption with split-pane display

Each view implements the Bubble Tea `Model` interface (`Init`, `Update`, `View`). View switching is handled by `appModel` which delegates to the active child model.

### RabbitMQ Integration

- `consumer.go` - AMQP 0-9-1 consumer using amqp091-go. Creates exclusive auto-delete queues by default, or uses existing queues. Supports durable queue creation.
- `management.go` - HTTP client for RabbitMQ Management API (port 15672). Used by browser view to list exchanges, queues, and bindings.

### Protobuf Decoding

The `proto.Decoder` dynamically loads `.proto` files from a directory and attempts to decode messages by:
1. Trying all known message types
2. Scoring by populated field count
3. Boosting score for types matching the routing key (e.g., `editorial.it.country.updated` → `CountryUpdated`)
