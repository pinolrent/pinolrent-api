# Imagen de despliegue de la API. El binario es estático (modernc.org/sqlite es
# Go puro, sin cgo), así que el runtime no necesita toolchain ni libc extra.

FROM golang:1.26.6-alpine AS build
# git solo en la etapa build: lo usa git describe para versionar el binario.
RUN apk add --no-cache git
WORKDIR /src

# Las dependencias primero: así el cache de capas no se invalida con cada
# cambio en el código.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Versión que reporta GET /health: si no se pasa --build-arg, sale de
# git describe sobre el clone (requiere .git en el contexto de build, así que
# no va en .dockerignore). Solo la etapa build lo usa; la imagen final solo
# recibe el binario.
ARG VERSION=
RUN V="${VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"; \
	CGO_ENABLED=0 go build -ldflags "-X main.version=${V}" -o /out/pinolrent-api ./cmd/api

FROM alpine:3.22
# sqlite (CLI) es para el snapshot diario de la base; su-exec baja privilegios
# en el entrypoint.
RUN apk add --no-cache ca-certificates sqlite su-exec \
	&& adduser -D -u 10001 app

COPY --from=build /out/pinolrent-api /usr/local/bin/pinolrent-api
COPY deploy/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

# /data es el punto de montaje del volumen persistente: ahí viven la base
# SQLite (con sus -wal/-shm) y las imágenes subidas.
ENV DATABASE_URL=/data/pinolrent.db \
	UPLOAD_DIR=/data/uploads \
	PORT=8080

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
	CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

# El entrypoint arranca como root para poder ajustar el dueño del volumen
# (Docker lo crea como root) y después baja a app.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/pinolrent-api"]
