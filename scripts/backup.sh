#!/bin/sh
# Daily SQLite snapshot for PinolRent. A file-level copy of a database that is
# being written can come out inconsistent, so the snapshot goes through the
# SQLite engine (VACUUM INTO over the live database): the result is a
# complete, compact copy. Snapshots older than 14 days are pruned.
#
# Usage (daily cron on the host that holds /data):
#   0 3 * * *  DATABASE_URL=/data/pinolrent.db /opt/pinolrent-api/scripts/backup.sh
#
# Off-site copy is the operator's job: sync /data/backups and the uploads dir
# to S3-compatible storage, e.g.
#   aws s3 sync /data/backups s3://my-bucket/pinolrent-backups --delete
set -eu

DB="${DATABASE_URL:-/data/pinolrent.db}"
BACKUP_DIR="${BACKUP_DIR:-/data/backups}"
TODAY="$(date +%F)"

mkdir -p "$BACKUP_DIR"
rm -f "$BACKUP_DIR/pinolrent-$TODAY.db"
sqlite3 "$DB" "VACUUM INTO '$BACKUP_DIR/pinolrent-$TODAY.db'"
find "$BACKUP_DIR" -type f -name '*.db' -mtime +14 -delete
