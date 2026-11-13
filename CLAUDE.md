# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build
go build -o mittere ./cmd/mittere

# Run (default config: config.yml in working directory)
./mittere -conf config.yml -log /var/log/mittere

# Run tests (integration tests need a real MongoDB, see below)
go test -race ./...

# Skip the integration tests
go test -short ./...
```

The binary entry point is `cmd/mittere/main.go`. There is no Makefile or Dockerfile.

**Deployment**: `.github/workflows/deploy.yml` deploys on every push to `master`
— it builds the binary, copies it and the rendered `mittere-config.yml` to the
server behind `secrets.SERVER_IP` and restarts `mittere.service`. That server is
the live Wattbrews instance (notify.wattbrews.me). Do not push to `master`
without saying what will be deployed.

## Tests

Table-driven unit tests for routing, the rate limiter and the HTTP contract;
integration tests against a **real MongoDB**, never a mock. The address comes
from `MONGO_URI` (default `mongodb://127.0.0.1:27017`); each test uses its own
database and drops it afterwards. `internal/lib/memrepo` holds the in-memory
repository and transport the unit tests build on.

## Configuration

YAML config loaded via `cleanenv` (see `config.yml`). Key flags:
- `-conf` — path to config file (default: `config.yml`)
- `-log` — log directory (default: `/var/log/mittere`)

MongoDB and Telegram are switched on by `enabled: true` in their sections. An
empty `mongo.user` means a database without authentication. A failed MongoDB
connection stops the service: a relay that cannot journal must not pretend to
work.

## Architecture

Mittere is a notification relay: it takes messages over HTTP and delivers them
to Telegram chats subscribed to a topic.

```
cmd/mittere/main.go          — bootstrap: config, logger, DB migration + indexes, bot, HTTP server
impl/core/                   — business logic: auth, routing, dispatch, admin, chat commands
impl/telegram/               — Telegram transport: sending, per-chat rate limiting, bot commands
internal/database/           — MongoDB: one file per collection, plus the v1 to v2 migration
internal/http-server/        — chi router, middleware, handlers
entity/                      — domain types
internal/lib/                — logger, validator, response helpers, context, memrepo
```

### Domain model (v2)

| Collection | What it holds |
|---|---|
| `channels` | a chat that receives: private chat, group, or forum topic (`chat_id` + `thread_id`) |
| `topics` | named streams, code like `marketing.shipments`; `default` and `admin` are built in |
| `subscriptions` | topic ↔ channel, many to many |
| `senders` | API clients: token, scope (`send`/`admin`), allowed topics |
| `events` | the delivery journal, one record per published message, TTL 90 days |
| `link_codes` | one-off codes for linking a chat to a topic |
| `users_v1` | the untouched v1 documents, kept by the migration |

A person's private chat is a channel like any other: there is no separate role
logic. The v1 schema kept subscriber, recipient and sender as one `entity.User`
in the `subscriptions` collection; `internal/database/migrate.go` rebuilds that
and runs once, before the indexes are created.

### Delivery

`core.Publish` resolves the topic, reads the subscribed active channels, sends to
each through the transport and answers only when every delivery has been
attempted. The journal record carries the outcome per channel. The HTTP status
says what happened: 200 all, 207 some, 409 nobody subscribed, 502 none. This is
the point of v2 — 1C marks a notification delivered on 200, so a 200 must mean
it arrived.

`dedup_key` makes a retry safe: the recipients that already got the message are
skipped, and a fully delivered event is never sent again.

The transport owns rate limiting (per chat, 20 a minute by default) and the 429
retry (`retry_after`, one repeat); it reports the number of attempts so the
journal records them.

### Key interfaces

- `core.Repository` — data access, implemented by `database.MongoDB` and by `memrepo.Repo`
- `core.Transport` — message delivery, implemented by `telegram.TgBot` and by `memrepo.Transport`
- `telegram.Commands` — the logic behind the bot commands, implemented by `core.Core`
- `api.Handler` — the composite the router needs

### HTTP API

Full description in `docs/api.md`. Sending needs a `send` token, everything else
an `admin` token; `GET /health` needs none.

| Method | Path | Scope |
|---|---|---|
| POST | /messages | send |
| POST | /tg/msg, GET /tg/test | send (legacy) |
| GET/POST | /topics, /senders | admin |
| GET/PUT/DELETE | /topics/{code}, /senders/{username} | admin |
| GET | /channels, /events, /events/{id} | admin |
| POST | /channels/link | admin |
| GET/PUT/DELETE | /channels/{id} | admin |
| GET | /health | none (admin sees more) |

### Telegram bot

`github.com/go-telegram/bot` (v1.20.0) with a default handler. Commands:
`/link CODE`, `/start`, `/status`, `/stop`, `/test`. A chat is linked by sending
a code in it, so no chat identifier is ever typed by hand and the bot's privacy
mode in groups is not in the way. Forum topics are supported through
`message_thread_id`.
