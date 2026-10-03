#!/bin/sh
set -e

# If DATABASE_URL is not set, assemble it from individual DB_* environment variables
if [ -z "$DATABASE_URL" ] && [ -n "$DB_HOST" ]; then
  DB_SSLMODE="${DB_SSLMODE:-disable}"
  DB_PORT="${DB_PORT:-5432}"
  DB_USER="${DB_USER:-orchestrix}"
  DB_NAME="${DB_NAME:-orchestrix_dev}"
  export DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}"
fi

# Run database migrations if enabled (default true) and DATABASE_URL is present
if [ "${RUN_MIGRATIONS:-true}" = "true" ] && [ -n "$DATABASE_URL" ]; then
  echo "Checking database connection and applying pending migrations..."
  retries=20
  until /usr/local/bin/migrate -path=/app/migrations -database "$DATABASE_URL" up || [ $retries -le 0 ]; do
    echo "Waiting for database to accept connections ($retries attempts remaining)..."
    retries=$((retries - 1))
    sleep 2
  done

  if [ $retries -le 0 ]; then
    echo "Error: Failed to apply migrations after multiple retries."
    exit 1
  fi
  echo "Database migrations are up to date."
fi

# Execute the main container command (defaults to ./orchestrix)
exec "$@"
