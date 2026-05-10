# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/zyna-presence ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
	&& addgroup -S zyna \
	&& adduser -S -G zyna -H -h /nonexistent zyna \
	&& mkdir -p /data \
	&& chown zyna:zyna /data

COPY --from=build /out/zyna-presence /usr/local/bin/zyna-presence

USER zyna:zyna
WORKDIR /data

ENV PORT=8080 \
	LAST_SEEN_FILE=/data/last_seen.json

EXPOSE 8080
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
	CMD wget -q -O - "http://127.0.0.1:${PORT}/health" >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/zyna-presence"]
