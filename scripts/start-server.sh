#!/usr/bin/env bash
set -euo pipefail

# Simple helper to build and run FAS with common defaults.
# Usage:
#   ./scripts/start-server.sh [--auth secret] [--tls-cert cert.pem --tls-key key.pem] [extra fas flags...]

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/bin/fas"

if [ ! -x "$BIN" ]; then
  echo "Building fas..."
  (cd "$ROOT" && go build -o "$BIN" ./cmd/fas)
fi

EXTRA=()
AUTH_ENABLED=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --auth)
      AUTH_ENABLED=true
      PASSWORD="$2"
      shift 2
      ;;
    *)
      EXTRA+=("$1")
      shift
      ;;
  esac
done

CMD=("$BIN" -host 0.0.0.0 -port 6379 -aof fas.aof -rdb fas.rdb -fsync everysec)

if $AUTH_ENABLED; then
  CMD+=(-auth -requirepass "$PASSWORD")
fi

CMD+=("${EXTRA[@]}")

echo "Starting server: ${CMD[*]}"
exec "${CMD[@]}"
