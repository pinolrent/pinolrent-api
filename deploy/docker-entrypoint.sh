#!/bin/sh
# Arranque del contenedor. Docker (y Coolify) crean el volumen persistente como
# root, así que la app —que corre sin privilegios— necesita que el directorio de
# datos sea suyo antes de tocar la base SQLite. Después de eso cede el control
# al proceso principal, que recibe SIGTERM directamente desde el runtime.
set -e

mkdir -p "$(dirname "$DATABASE_URL")" "$UPLOAD_DIR" /data/backups
chown -R app:app /data

exec su-exec app "$@"
