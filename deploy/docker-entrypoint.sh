#!/bin/sh
# Container startup. Docker creates the persistent volume as root, so the app
# —which runs unprivileged— needs the data directory to belong to it before
# touching the SQLite database. After that it hands control over to the main
# process, which receives SIGTERM straight from the runtime.
set -e

mkdir -p "$(dirname "$DATABASE_URL")" "$UPLOAD_DIR" /data/backups
chown -R app:app /data

exec su-exec app "$@"
