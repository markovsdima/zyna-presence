# Zyna Presence Server

Lightweight presence tracking service for the [Zyna](https://github.com/markovsdima/Zyna/tree/develop) Matrix client. Tracks online/offline status and last seen time via heartbeat polling.

## How it works

Clients poll every ~10 seconds. Each heartbeat writes a timestamp to Redis with a short TTL (default 30s). When heartbeats stop, the key expires and the user is considered offline. A separate long-lived key (30 days) preserves the last seen timestamp for offline users.

## API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `PUT` | `/presence/{userID}` | Send heartbeat (marks user online) |
| `POST` | `/presence/status` | Batch query status for up to 200 users |
| `GET` | `/health` | Health check (no auth required) |

All endpoints except `/health` require an `X-API-Key` header (HMAC-SHA256 of today's UTC date).

## Quick start

```bash
# Start Redis
brew services start redis

# Run the server
HMAC_SECRET=your-secret-here go run ./cmd/server
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `PRESENCE_TTL` | `30s` | How long a heartbeat keeps user online |
| `HMAC_SECRET` | | **Required.** Shared secret for API key generation |
| `RATE_LIMIT_IP` | `60` | Max requests per minute per IP |
| `RATE_LIMIT_HEARTBEAT` | `5s` | Min interval between heartbeats per user |

## Project structure

```
cmd/server/main.go           - Entry point
internal/
  config/config.go           - Configuration from ENV
  handler/presence.go        - HTTP handlers
  service/presence.go        - Business logic
  storage/redis.go           - PresenceStore interface + Redis implementation
  middleware/hmac.go          - HMAC-based API key verification
  middleware/ratelimit.go     - Rate limiting (by IP + by user)
```

## Tech stack

- Go 1.22+
- Redis
- [chi](https://github.com/go-chi/chi) for routing
- [go-redis](https://github.com/redis/go-redis) for Redis
- `log/slog` for structured logging
