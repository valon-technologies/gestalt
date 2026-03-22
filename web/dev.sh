#!/usr/bin/env bash
# Development server script for gestalt web UI.
#
# Usage:
#   ./dev.sh [web|full] [config.yaml]
#
# Modes:
#   web  — Next.js with mock API, no Go server needed (default)
#   full — Go API server + Next.js, proxied together
#
# Examples:
#   ./dev.sh                              # mock mode
#   ./dev.sh full                         # uses gestalt.dev.yaml (defaults)
#   ./dev.sh full gestalt.local.yaml      # uses your custom config
#   API_PORT=9090 ./dev.sh full           # custom port

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

MODE="${1:-web}"

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

case "$MODE" in
    web)
        info "Starting Next.js dev server on port $WEB_PORT (mock API)..."
        info "Using built-in mock API routes — no Go server required."
        info "To proxy to a real Go server: ./dev.sh full"
        npx next dev --port "$WEB_PORT"
        ;;

    full)
        CONFIG="${2:-${GESTALT_CONFIG:-$GESTALT_DIR/gestalt.dev.yaml}}"
        if [[ "$CONFIG" != /* ]]; then
            CONFIG="$GESTALT_DIR/$CONFIG"
        fi

        if [[ ! -f "$CONFIG" ]]; then
            err "Config not found: $CONFIG"
            echo ""
            echo "Quick start options:"
            echo "  1. Use the built-in dev config:  ./dev.sh full"
            echo "  2. Use a custom config:          ./dev.sh full path/to/config.yaml"
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

        info "Starting Next.js dev server on port $WEB_PORT..."
        info "API proxy: /api/v1/* -> http://localhost:$API_PORT"
        GESTALT_API_URL="http://localhost:$API_PORT" \
            npx next dev --port "$WEB_PORT" &
        WEB_PID=$!

        ok "Ready: http://localhost:$WEB_PORT"
        echo ""
        wait
        ;;

    *)
        err "Unknown mode: $MODE"
        echo ""
        echo "Usage: ./dev.sh [web|full] [config.yaml]"
        echo ""
        echo "Modes:"
        echo "  web   Start Next.js with mock API (default)"
        echo "  full  Start Go server + Next.js"
        echo ""
        echo "Examples:"
        echo "  ./dev.sh                              # mock mode"
        echo "  ./dev.sh full                         # default dev config"
        echo "  ./dev.sh full gestalt.local.yaml      # custom config"
        exit 1
        ;;
esac
