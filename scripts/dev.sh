#!/usr/bin/env bash
# Dev bootstrap: fills in safe defaults when no .env exists, then runs the API.
# Production fail-fast is preserved: running `go run ./cmd/api` directly
# without JWT_SECRET still exits with an error.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -f .env ]; then
  if [ -z "${JWT_SECRET:-}" ]; then
    export JWT_SECRET="dev-secret-not-for-production"
  fi
  if [ -z "${DATABASE_URL:-}" ]; then
    export DATABASE_URL="dev.db"
  fi
  echo "dev.sh: no .env found, using dev defaults (JWT_SECRET/DATABASE_URL=dev.db)."
  echo "dev.sh: copy .env.example to .env and edit it to override them."
else
  echo "dev.sh: loading .env"
fi

exec go run ./cmd/api