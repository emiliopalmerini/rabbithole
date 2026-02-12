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
- **SQLite Persistence** - Optionally save messages to a local database for history and replay
- **Session History** - Auto-load messages from previous sessions when persistence is enabled
- **Search & Filter** - Search through messages with vim-style keybindings (`/`, `n`, `N`)
- **Bookmarks** - Mark important messages for quick reference
- **Export & Yank** - Export messages or copy to clipboard

## Installation

```bash
go install github.com/epalmerini/rabbithole@latest
```

Or build from source:

```bash
git clone https://github.com/epalmerini/rabbithole.git
cd rabbithole
make          # or: go build -o rabbithole .
```

### Build Targets

A `Makefile` is provided for common tasks:

```bash
make          # Format, vet, test, and build
make build    # Build the binary
make test     # Run tests
make clean    # Remove build artifacts
make help     # Show all available targets
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

### Message Persistence

Enable SQLite persistence to save all consumed messages for later analysis:

```bash
# Enable persistence with default database location
rabbithole -exchange events -persist

# Use a custom database path
rabbithole -exchange events -persist -db /path/to/messages.db
```

Messages are saved asynchronously via a buffered channel to avoid impacting consumption performance. The database is stored at `~/.local/share/rabbithole/rabbithole.db` by default.

Each session records:
- Session metadata (exchange, routing key, queue, timestamps)
- Full message content (body, headers, AMQP properties)
- Detected protobuf type (if proto decoding is enabled)

The database includes FTS5 full-text search on message bodies and routing keys.

**Session History**: When persistence is enabled, rabbithole automatically loads messages from your last session on the same exchange. Historical messages are displayed with a muted style and marked with `H` (historical) vs `L` (live) in the status bar.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `amqp://guest:guest@localhost:5672/` | RabbitMQ connection URL |
| `-exchange` | | Exchange to bind to (omit for browser mode) |
| `-routing-key` | `#` | Routing key pattern (`#` = all, `*` = single word) |
| `-queue` | | Queue name (empty = auto-generated exclusive queue) |
| `-proto` | | Path to directory containing `.proto` files |
| `-persist` | `false` | Enable SQLite message persistence |
| `-db` | `~/.local/share/rabbithole/rabbithole.db` | Custom database path |
| `-management-url` | (auto-detected from `-url`) | Override RabbitMQ Management API URL |
| `-version` | | Show version and exit |

## Keybindings

### Consumer View

#### Navigation
| Key | Action |
|-----|--------|
| `↑` / `k` | Move selection up |
| `↓` / `j` | Move selection down |
| `gg` | Jump to first message |
| `G` | Jump to last message |
| `zz` | Center current line |
| `5j` / `3k` | Move by count (vim-style numeric prefixes) |

#### Search
| Key | Action |
|-----|--------|
| `/` | Start search (type query, press Enter) |
| `n` | Next search result |
| `N` | Previous search result |
| `Esc` | Exit search mode |

#### Actions
| Key | Action |
|-----|--------|
| `r` | Toggle raw/decoded view |
| `p` / `Space` | Pause/resume consuming |
| `c` | Clear all messages |
| `y` | Yank (copy) current message to clipboard |
| `e` | Export all messages |
| `m` | Toggle bookmark on current message |
| `'` | Jump to next bookmark |

#### View
| Key | Action |
|-----|--------|
| `t` | Toggle compact mode |
| `T` | Toggle relative/absolute timestamps |
| `H` | Resize pane left (wider detail) |
| `L` | Resize pane right (wider list) |
| `?` | Toggle help overlay |

#### Navigation
| Key | Action |
|-----|--------|
| `b` | Back to topology browser |
| `q` | Quit |

### Topology Browser

| Key | Action |
|-----|--------|
| `↑` / `k` | Move selection up |
| `↓` / `j` | Move selection down |
| `Enter` | Select exchange/binding |
| `/` | Filter exchanges/bindings (type to search) |
| `Esc` | Go back / Exit filter mode |
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
