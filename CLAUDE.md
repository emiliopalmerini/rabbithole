# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
# Build (using Makefile)
make          # Format, vet, test, and build
make build    # Just build
make test     # Run tests
make clean    # Remove artifacts

# Build (using go directly)
go build -o rabbithole .

# Run (browser mode - shows topology explorer)
./rabbithole -url "amqp://user:pass@host:5672/"

# Run (direct consumer mode - bypasses browser)
./rabbithole -exchange myexchange -routing-key "#"

# Run with protobuf decoding
./rabbithole -exchange myexchange -proto /path/to/proto/files

# Run with SQLite persistence
./rabbithole -exchange myexchange -persist

# Run with custom database path
./rabbithole -exchange myexchange -persist -db /path/to/messages.db

# Run with custom management API URL
./rabbithole -url "amqp://user:pass@host:5672/" -management-url "http://host:15672"

# Install globally
go install github.com/epalmerini/rabbithole@latest
```

## Architecture

This is a TUI application for consuming and inspecting RabbitMQ messages, built with the Bubble Tea framework.

### Package Structure

- `main.go` - Entry point, CLI flag parsing
- `internal/tui/` - TUI layer using Bubble Tea (Elm architecture: Model-Update-View)
  - `keyhandler.go` - Vim-style keybinding handler with multi-key sequences and numeric prefixes
  - `model.go` - Consumer view with search, bookmarks, yank, export
  - `browser.go` - Topology browser with filtering
  - `app.go` - Main app model that switches between views
- `internal/rabbitmq/` - RabbitMQ integration (AMQP consumer + Management API client)
- `internal/proto/` - Dynamic protobuf decoding using bufbuild/protocompile
- `internal/db/` - SQLite persistence layer (sqlc-generated queries + async writer)
- `Makefile` - Build automation (fmt, vet, test, build)

### TUI Architecture (Bubble Tea)

The app has two main views managed by `appModel`:
1. **Browser view** (`browserModel`) - Topology explorer showing exchanges/bindings, allows creating queues. Supports filtering with `/` key.
2. **Consumer view** (`model`) - Real-time message consumption with split-pane display. Features vim-style navigation, search, bookmarks, yank/export, and multiple view modes.

Each view implements the Bubble Tea `Model` interface (`Init`, `Update`, `View`). View switching is handled by `appModel` which delegates to the active child model.

**Vim-style keybindings**: The consumer view uses a stateful key handler (`VimKeyState` in `keyhandler.go`) that supports:
- Multi-key sequences (e.g., `gg`, `zz`)
- Numeric prefixes (e.g., `5j` to move down 5 lines)
- Key timeout (500ms) for sequence completion

**Search**: Both views support search mode (activated with `/`). In consumer view, search results are highlighted and navigable with `n`/`N` keys.

### RabbitMQ Integration

- `consumer.go` - AMQP 0-9-1 consumer using amqp091-go. Creates exclusive auto-delete queues by default, or uses existing queues. Supports durable queue creation.
- `management.go` - HTTP client for RabbitMQ Management API (default port 15672, overridable with `-management-url` flag). Used by browser view to list exchanges, queues, and bindings. URL is auto-detected from AMQP connection URL.

### Protobuf Decoding

The `proto.Decoder` uses `bufbuild/protocompile` (migrated from deprecated `jhump/protoreflect`) to dynamically load `.proto` files from a directory and attempts to decode messages by:
1. Trying all known message types
2. Scoring by populated field count
3. Boosting score for types matching the routing key (e.g., `editorial.it.country.updated` → `CountryUpdated`)

The decoder uses the modern `protoreflect` API with `dynamicpb.NewMessage` for dynamic message creation.

### SQLite Persistence

Optional message persistence using SQLite (pure Go, no CGO via modernc.org/sqlite):

- `schema.sql` - Database schema with sessions, messages tables, and FTS5 full-text search
- `query.sql` - sqlc queries for CRUD operations
- `store.go` - `Store` interface and `SQLiteStore` implementation
- `writer.go` - `AsyncWriter` with buffered channel (1000 msgs) for non-blocking writes

The persistence flow:
1. On connection, a new session is created with exchange/routing key metadata
2. **Historical messages are loaded** from the most recent ended session with matching exchange/routing key
3. Each consumed message is queued to `AsyncWriter.Save()` (non-blocking, drops if buffer full)
4. Background goroutine persists messages to SQLite
5. On shutdown, buffer is drained and session is marked as ended

**Session history**: Messages are displayed with indicators:
- `H` = Historical (loaded from previous session, shown with muted style)
- `L` = Live (received in current session)
- Status bar shows breakdown (e.g., "5H+3L messages")

Database location: `~/.local/share/rabbithole/rabbithole.db` (follows XDG spec)

### Consumer View Features

The consumer view (`model.go`) includes:
- **Search**: `/` to search messages, `n`/`N` to navigate results, fuzzy matching on routing key and body
- **Bookmarks**: `m` to toggle bookmark, `'` to jump to next bookmarked message
- **Yank**: `y` to copy current message (raw or decoded) to system clipboard
- **Export**: `e` to export all messages to a file (JSON format with timestamp)
- **View modes**:
  - `r` - Toggle raw/decoded protobuf view
  - `t` - Toggle compact mode (hide headers)
  - `T` - Toggle relative/absolute timestamps
  - `?` - Toggle help overlay
- **Pane resizing**: `H`/`L` to adjust split between message list and detail pane
- **Vim navigation**: `gg`, `G`, `zz`, numeric prefixes (e.g., `5j`)
