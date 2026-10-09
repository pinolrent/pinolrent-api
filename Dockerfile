# Deployment image for the API. The binary is static (modernc.org/sqlite is
# pure Go, no cgo), so the runtime needs no toolchain and no extra libc.

FROM golang:1.26.9-alpine AS build
# git only in the build stage: git describe uses it to version the binary.
RUN apk add --no-cache git
WORKDIR /src

# Dependencies first: that way the layer cache is not invalidated on every
# code change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Version reported by GET /health: pass it with --build-arg VERSION=... (CI
# passes ci-<sha>). Without it the build tries git describe, which finds no
# .git in the build context (it is in .dockerignore) and falls back to dev.
# Only the build stage uses it; the final image only receives the binary.
ARG VERSION=
RUN V="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"; \
	CGO_ENABLED=0 go build -ldflags "-X main.version=${V}" -o /out/pinolrent-api ./cmd/api

FROM alpine:3.22
# sqlite (CLI) is for the daily database snapshot; su-exec drops privileges
# in the entrypoint.
RUN apk add --no-cache ca-certificates sqlite su-exec \
	&& adduser -D -u 10001 app

COPY --from=build /out/pinolrent-api /usr/local/bin/pinolrent-api
COPY deploy/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# /data is the mount point of the persistent volume: the SQLite database
# (with its -wal/-shm) and the uploaded images live there.
ENV DATABASE_URL=/data/pinolrent.db \
	UPLOAD_DIR=/data/uploads \
	PORT=8080

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD wget -q -O /dev/null http://127.0.0.1:${PORT:-8080}/health || exit 1

# The entrypoint starts as root to be able to fix the volume owner
# (Docker creates it as root) and then drops to app.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/pinolrent-api"]
