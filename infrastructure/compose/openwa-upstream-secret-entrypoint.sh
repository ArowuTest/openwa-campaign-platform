#!/bin/sh
set -eu

read_secret() {
  name="$1"
  path="$2"
  if [ ! -f "$path" ]; then
    echo "required OpenWA secret file is missing: $name" >&2
    exit 2
  fi
  value=$(cat "$path")
  if [ -z "$value" ]; then
    echo "required OpenWA secret file is empty: $name" >&2
    exit 2
  fi
  export "$name=$value"
}

read_secret API_MASTER_KEY "${API_MASTER_KEY_FILE:-/run/secrets/openwa_upstream_api_key}"
read_secret API_KEY_PEPPER "${API_KEY_PEPPER_FILE:-/run/secrets/openwa_api_key_pepper}"
exec node dist/main
