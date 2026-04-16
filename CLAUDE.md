# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build
go build -o mittere ./cmd/mittere

# Run (default config: config.yml in working directory)
./mittere -conf config.yml -log /var/log/mittere

# Run tests
go test ./...

# Run a single package's tests
go test ./internal/lib/validate/...
```

No Makefile, Dockerfile, or CI pipeline exists. The binary entry point is `cmd/mittere/main.go`.

## Configuration

YAML config loaded via `cleanenv` (see `config.yml` for structure). Key flags:
- `-conf` — path to config file (default: `config.yml`)
- `-log` — log directory (default: `/var/log/mittere`)

Both MongoDB and Telegram are optional and controlled by `enabled: true/false` in config.

## Architecture

Mittere is a notification relay service — it receives messages via HTTP API and dispatches them to Telegram subscribers.

### Layer overview

```
cmd/mittere/main.go          — bootstrap: config, logger, DB, Telegram bot, HTTP server
impl/core/core.go            — business logic hub; owns auth + message dispatch
impl/telegram/               — Telegram bot (long-polling updates, subscription mgmt, message sending)
internal/http-server/api/    — chi router setup, middleware wiring, route registration
internal/http-server/handlers/ — HTTP handler functions (service test endpoints, telegram notify)
internal/http-server/middleware/ — authenticate (Bearer token) and timeout middleware
internal/database/           — MongoDB client (users, subscriptions collections)
entity/                      — domain types: User, Subscription, EventMessage
internal/lib/                — small utilities: logger, validator, response helpers, context helpers
```

### Key interfaces

- `core.Repository` — data access (implemented by `database.MongoDB`)
- `core.MessageService` — message dispatch (implemented by `telegram.TgBot`)
- `api.Handler` — composite interface aggregating auth + service + telegram handler capabilities

### HTTP API

All routes require `Authorization: Bearer <token>` header. Routes defined in `api/api.go`:

| Method | Path               | Purpose                    |
|--------|--------------------|----------------------------|
| GET    | /tg/test           | Send test Telegram event   |
| POST   | /tg/msg            | Send Telegram notification |
| GET    | /users/            | List all users             |
| POST   | /users/            | Create a user              |
| GET    | /users/{username}  | Get a user by username     |
| PUT    | /users/{username}  | Update a user              |
| DELETE | /users/{username}  | Delete a user              |

### Telegram bot

Uses `github.com/go-telegram/bot` (v1.20.0) with a default handler pattern (`handleUpdate`). Commands: `/start`, `/stop`, `/test`, `/invite`, `/clear`, `/list`. The bot runs via `bot.Start(ctx)` in a goroutine; shutdown is via context cancellation. Subscriptions are stored in MongoDB and cached in-memory (`map[int64]entity.Subscription`, mutex-protected). Messages are dispatched through buffered channels (`event` for broadcast, `send` for direct). MarkdownV2 escaping uses `bot.EscapeMarkdown()`.

### Auth flow

The `authenticate` middleware extracts a Bearer token, then `core.AuthenticateByToken` checks: (1) static `auth_token` from config, (2) MongoDB user lookup. Authenticated user is stored in request context via `cont.PutUser`.
