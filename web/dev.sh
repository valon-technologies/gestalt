#!/usr/bin/env bash
# Frontend hot-reload development script.
#
# Starts gestaltd and the Next.js dev server together so frontend
# changes are reflected immediately without rebuilding.
#
# Usage:
#   ./dev.sh [config.yaml]
#
# Examples:
#   ./dev.sh                              # uses gestalt.dev.yaml
#   ./dev.sh gestalt.local.yaml           # custom config
#   API_PORT=9090 WEB_PORT=4000 ./dev.sh  # custom ports

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GESTALT_DIR="$SCRIPT_DIR/.."
WEB_PORT="${WEB_PORT:-3000}"
API_PORT="${API_PORT:-8080}"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
NC='\033[0m'

info()  { echo -e "${BLUE}==> $1${NC}"; }
ok()    { echo -e "${GREEN}==> $1${NC}"; }
warn()  { echo -e "${YELLOW}==> $1${NC}"; }
err()   { echo -e "${RED}==> $1${NC}" >&2; }

cleanup() {
    if [[ -n "${API_PID:-}" ]]; then
        kill "$API_PID" 2>/dev/null || true
    fi
    if [[ -n "${WEB_PID:-}" ]]; then
        kill "$WEB_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

cd "$SCRIPT_DIR"

if [[ ! -d node_modules ]]; then
    info "Installing npm dependencies..."
    npm install
fi

if [[ -f "$GESTALT_DIR/.env" ]]; then
    info "Loading $GESTALT_DIR/.env"
    set -a
    # shellcheck disable=SC1091
    source "$GESTALT_DIR/.env"
    set +a
fi

CONFIG="${1:-${GESTALT_CONFIG:-$GESTALT_DIR/gestalt.dev.yaml}}"
if [[ "$CONFIG" != /* ]]; then
    CONFIG="$GESTALT_DIR/$CONFIG"
fi

if [[ ! -f "$CONFIG" ]]; then
    err "Config not found: $CONFIG"
    echo ""
    echo "Quick start options:"
    echo "  1. Use the built-in dev config:  ./dev.sh"
    echo "  2. Use a custom config:          ./dev.sh path/to/config.yaml"
    echo "  3. Copy and customize:           cp gestalt.dev.yaml gestalt.local.yaml"
    exit 1
fi

if ! command -v go &>/dev/null; then
    err "Go is not installed. Install it from https://go.dev/dl/"
    exit 1
fi

info "Config: $CONFIG"
info "Starting Go API server on port $API_PORT..."
warn "Dev mode — use 'Dev Login' on the login page (no Google OAuth needed)."
(cd "$GESTALT_DIR" && go run ./cmd/gestaltd --config "$CONFIG") &
API_PID=$!

API_READY=false
for i in $(seq 1 30); do
    if curl -sf "http://localhost:$API_PORT/health" >/dev/null 2>&1; then
        ok "Go server ready on port $API_PORT"
        API_READY=true
        break
    fi
    if ! kill -0 "$API_PID" 2>/dev/null; then
        err "Go server exited unexpectedly. Check your config: $CONFIG"
        wait "$API_PID" || true
        exit 1
    fi
    sleep 0.5
done
if [[ "$API_READY" != "true" ]]; then
    err "Go server did not become ready within 15 seconds"
    exit 1
fi

info "Starting Next.js dev server on port $WEB_PORT (hot reload)..."
info "API: http://localhost:$API_PORT  Web: http://localhost:$WEB_PORT"
NEXT_PUBLIC_API_URL="http://localhost:$API_PORT" \
    npx next dev --port "$WEB_PORT" &
WEB_PID=$!

ok "Ready: http://localhost:$WEB_PORT"
echo ""
wait
