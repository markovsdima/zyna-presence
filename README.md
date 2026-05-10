# Zyna Presence Server

Lightweight presence tracking service for the [Zyna](https://github.com/markovsdima/Zyna/tree/develop) Matrix client. Tracks online/offline status and last seen time over WebSocket.

## How it works

Clients exchange a Matrix access token for a short-lived JWT via REST, then open a persistent WebSocket connection. The server tracks which users are online in memory with multi-device support: a user is "online" when at least one device is connected and "offline" when the last device disconnects. Subscribers receive real-time presence updates. Last seen timestamps are persisted to a JSON file.

## API

### REST

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/presence/auth` | Exchange Matrix token for JWT |
| `GET` | `/health` | Health check |

### WebSocket

Connect to `/presence/ws`, then authenticate with the JWT as the first message.

**Client -> Server:**

| Type | Description |
|------|-------------|
| `auth` | Authenticate or refresh JWT: `{type: "auth", token: "..."}` |
| `subscribe` | Subscribe to users (max 200): `{type: "subscribe", user_ids: [...]}` |
| `ping` | Heartbeat to keep connection alive |

**Server -> Client:**

| Type | Description |
|------|-------------|
| `presence` | Status change: `{type: "presence", user_id: "...", online: bool, last_seen?: timestamp}` |
| `statuses` | Initial statuses after subscribe: `{type: "statuses", users: [...]}` |
| `token_expired` | JWT expired, client should re-auth |
| `error` | Error message |

## Quick start

```bash
JWT_SECRET=your-secret-at-least-32-chars-long go run ./cmd/server
```

Dev mode (no Synapse needed):

```bash
AUTH_MODE=dev DEV_PASSWORD=secret JWT_SECRET=your-secret-at-least-32-chars-long go run ./cmd/server
```

## Docker

Build the image:

```bash
docker build -t zyna-presence:local .
```

Run it as a standalone container:

```bash
docker volume create zyna-presence-data
docker run --rm --name zyna-presence \
  -p 8080:8080 \
  -e JWT_SECRET=your-secret-at-least-32-chars-long \
  -e SYNAPSE_URL=http://host.docker.internal:8008 \
  -v zyna-presence-data:/data \
  zyna-presence:local
```

The image runs as a non-root user, listens on `PORT` (`8080` by default), and stores presence data at `/data/last_seen.json`. Mount `/data` as a volume in Docker Compose so `last_seen` data survives container recreation.

When Synapse runs in the same Docker Compose project, set `SYNAPSE_URL` to the Synapse service name, for example `http://synapse:8008`. When Synapse runs directly on the host, use the host address reachable from Docker, such as `http://host.docker.internal:8008` on Docker Desktop.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `JWT_SECRET` | | **Required.** Signing key for JWT (min 32 chars) |
| `SYNAPSE_URL` | `http://localhost:8008` | Matrix homeserver URL |
| `LAST_SEEN_FILE` | `last_seen.json` | File for persisting last seen timestamps. The Docker image sets `/data/last_seen.json` |
| `AUTH_MODE` | `synapse` | `synapse` or `dev` |
| `DEV_PASSWORD` | | Required when `AUTH_MODE=dev` |

## Project structure

```
cmd/server/main.go              - Entry point, routing, graceful shutdown
internal/
  config/config.go              - Configuration from ENV
  auth/jwt.go                   - JWT generation and validation
  auth/synapse.go               - Matrix homeserver token verification
  handler/auth.go               - POST /presence/auth + rate limiting
  handler/health.go             - GET /health
  presence/tracker.go           - In-memory presence tracking, subscriptions, broadcast
  presence/store.go             - Last seen persistence (JSON file)
  ws/handler.go                 - WebSocket lifecycle, auth, read loop
```

## Tech stack

- Go 1.22+
- [coder/websocket](https://github.com/coder/websocket) for WebSocket
- [chi](https://github.com/go-chi/chi) for routing
- [golang-jwt](https://github.com/golang-jwt/jwt) for JWT
- `log/slog` for structured logging
