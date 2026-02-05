# rabbithole

A beautiful TUI for consuming and inspecting RabbitMQ messages with Protobuf decoding.

![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)

## Features

- **Topology Browser** - Browse exchanges, view bindings, create queues interactively
- **Real-time Consumption** - Stream messages as they arrive with auto-scroll
- **Dynamic Protobuf Decoding** - Auto-detects message type from routing key
- **Split-pane View** - Message list on the left, details on the right
- **Hex View** - Toggle between decoded and raw hex view
- **Pause/Resume** - Freeze the stream to inspect messages
- **Durable Queues** - Create persistent queues that survive broker restarts

## Installation

```bash
go install github.com/epalmerini/rabbithole@latest
```

Or build from source:

```bash
git clone https://github.com/epalmerini/rabbithole.git
cd rabbithole
go build -o rabbithole .
```

## Usage

### Interactive Mode (Topology Browser)

Launch without specifying an exchange to browse your RabbitMQ topology:

```bash
rabbithole -url "amqp://user:pass@host:5672/"
```

This opens the topology browser where you can:
1. Select an exchange
2. View existing bindings or create a new queue
3. Set routing key pattern and durability
4. Start consuming

### Direct Consumer Mode

Skip the browser and consume directly:

```bash
# Consume from an exchange with routing key pattern
rabbithole -exchange my-exchange -routing-key "orders.#"

# Consume from an existing queue
rabbithole -queue my-existing-queue

# Connect to a specific RabbitMQ instance
rabbithole -url "amqp://user:pass@host:5672/" -exchange my-exchange
```

### Protobuf Decoding

Point to a directory containing `.proto` files for automatic message decoding:

```bash
rabbithole -exchange events -proto ./proto/
```

The decoder uses the routing key to guess the message type. For example, a message with routing key `editorial.it.country.updated` will preferentially match a `CountryUpdated` message type.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `amqp://guest:guest@localhost:5672/` | RabbitMQ connection URL |
| `-exchange` | | Exchange to bind to (omit for browser mode) |
| `-routing-key` | `#` | Routing key pattern (`#` = all, `*` = single word) |
| `-queue` | | Queue name (empty = auto-generated exclusive queue) |
| `-proto` | | Path to directory containing `.proto` files |
| `-version` | | Show version and exit |

## Keybindings

### Consumer View

| Key | Action |
|-----|--------|
| `↑` / `k` | Move selection up |
| `↓` / `j` | Move selection down |
| `g` | Jump to first message |
| `G` | Jump to last message |
| `r` | Toggle raw/decoded view |
| `p` / `Space` | Pause/resume consuming |
| `c` | Clear all messages |
| `b` | Back to topology browser |
| `q` | Quit |

### Topology Browser

| Key | Action |
|-----|--------|
| `↑` / `k` | Move selection up |
| `↓` / `j` | Move selection down |
| `Enter` | Select exchange/binding |
| `Esc` | Go back |
| `r` | Refresh topology |
| `q` | Quit |

### Create Queue Dialog

| Key | Action |
|-----|--------|
| `Tab` | Next field |
| `Shift+Tab` | Previous field |
| `Space` | Toggle durable checkbox |
| `Enter` | Create and start consuming |
| `Esc` | Cancel |

## Requirements

- Go 1.21+
- RabbitMQ with Management Plugin enabled (for topology browser)

## License

MIT
