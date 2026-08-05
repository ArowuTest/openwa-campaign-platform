#!/usr/bin/env sh
set -eu

for migration in /opt/campaign/migrations/*.sql; do
  echo "Applying migration: ${migration}"
  psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --file "$migration"
done

for seed in /opt/campaign/geography/*.sql; do
  echo "Applying reference data: ${seed}"
  psql --set ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --file "$seed"
done
