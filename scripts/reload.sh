#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="$PROJECT_DIR/keep-it-mobile"

echo "=== The Van Is Secure reload ==="
echo "→ Stopping existing process..."
pkill -f "$BINARY" 2>/dev/null && sleep 1 || echo "  (none running)"

if [ -f "$PROJECT_DIR/.env" ]; then
    set -a
    # shellcheck disable=SC1090
    source "$PROJECT_DIR/.env"
    set +a
fi

echo "→ Building..."
cd "$PROJECT_DIR"
if ! go build -o "$BINARY" ./cmd/keep-it-mobile/; then
  echo "✗ Build failed — fix compile errors before starting"
  exit 1
fi
echo "  Build OK: $BINARY"

PORT="${PORT:-8086}"
export PORT

if [ -z "${FRED_API_KEY:-}" ]; then
  echo "✗ FRED_API_KEY is not set — export it or add it to .env"
  exit 1
fi

echo "→ Starting on :${PORT}..."
nohup env PORT="$PORT" FRED_API_KEY="$FRED_API_KEY" "$BINARY" >"$PROJECT_DIR/keep-it-mobile.log" 2>&1 &
echo $! >"$PROJECT_DIR/keep-it-mobile.pid"

for i in $(seq 1 40); do
  sleep 0.2
  if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
    echo "✓ The Van Is Secure at http://127.0.0.1:${PORT}/"
    exit 0
  fi
done
echo "✗ Server did not respond — check keep-it-mobile.log"
exit 1
