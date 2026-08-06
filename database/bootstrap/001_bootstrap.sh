#!/usr/bin/env sh
set -eu

# Create NOLOGIN service identities before migrations so owner-scoped default
# privileges apply to every newly created relation. The script is idempotent and
# is executed again by the PostgreSQL image after migrations to reconcile grants.
role_bootstrap=/docker-entrypoint-initdb.d/002_service_roles.sql
if [ -f "$role_bootstrap" ]; then
  psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --file "$role_bootstrap"
fi

for migration in /opt/campaign/migrations/*.sql; do
  echo "Applying migration: ${migration}"
  psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --file "$migration"
done

for seed in /opt/campaign/geography/*.sql; do
  echo "Applying reference data: ${seed}"
  psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --file "$seed"
done

# Reconcile object-level grants after every migration and seed has created its
# relations. The first pass establishes roles and default privileges; this
# second pass is required for existing tables and keeps clean installs and
# upgrades aligned.
if [ -f "$role_bootstrap" ]; then
  psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --file "$role_bootstrap"
fi
