#!/usr/bin/env bash
# Dev bootstrap: fills in safe defaults when no .env exists, then runs the API.
# Production fail-fast is preserved: running `go run ./cmd/api` directly
# without JWT_SECRET still exits with an error.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ "${ENV:-}" = "prod" ] || [ "${ENV:-}" = "production" ]; then
  if [ -z "${JWT_SECRET:-}" ] && [ ! -f .env ]; then
    echo "dev.sh: refusing dev secret in prod (ENV=$ENV, no JWT_SECRET and no .env)" >&2
    exit 1
  fi
  if [ "${CORS_ALLOWED_ORIGINS:-}" = "*" ]; then
    echo "dev.sh: refusing CORS=* in prod (ENV=$ENV)" >&2
    exit 1
  fi
fi

if [ ! -f .env ]; then
  if [ -z "${JWT_SECRET:-}" ]; then
    export JWT_SECRET="dev-secret-not-for-production-32b"
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