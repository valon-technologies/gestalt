#!/usr/bin/env bash
# Client UI development script.
#
# Builds the client UI export and serves it through gestaltd so local
# development matches production routing and packaging.
#
# Usage:
#   ./dev.sh [config.yaml]
#
# Examples:
#   ./dev.sh                              # auto-generates ~/.gestaltd/config.yaml
#   ./dev.sh gestalt.local.yaml           # custom config
#   API_PORT=9090 ./dev.sh                # custom port

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GESTALTD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
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
}
trap cleanup EXIT

cd "$SCRIPT_DIR"

if [[ ! -d node_modules ]]; then
    info "Installing npm dependencies..."
    npm install
fi

if [[ -f "$REPO_DIR/.env" ]]; then
    info "Loading $REPO_DIR/.env"
    set -a
    # shellcheck disable=SC1091
    source "$REPO_DIR/.env"
    set +a
fi

CONFIG="${1:-${GESTALT_CONFIG:-}}"
if [[ -n "$CONFIG" ]]; then
    if [[ "$CONFIG" != /* ]]; then
        CONFIG="$REPO_DIR/$CONFIG"
    fi
    if [[ ! -f "$CONFIG" ]]; then
        err "Config not found: $CONFIG"
        exit 1
    fi
fi

if [[ -z "$CONFIG" && "$API_PORT" != "8080" ]]; then
    DEV_STATE_DIR="${HOME}/.gestaltd-dev/api-${API_PORT}"
    PROVIDERS_DIR="${GESTALT_PROVIDERS_DIR:-$GESTALTD_DIR/../../gestalt-providers}"
    mkdir -p "$DEV_STATE_DIR"
    KEY_FILE="$DEV_STATE_DIR/encryption_key"
    if [[ ! -f "$KEY_FILE" ]]; then
        if ! command -v openssl &>/dev/null; then
            err "openssl is required for custom API_PORT dev config generation"
            exit 1
        fi
        openssl rand -hex 32 > "$KEY_FILE"
    fi
    CONFIG="$DEV_STATE_DIR/config.yaml"
    cat > "$CONFIG" <<EOF
datastore:
  provider:
    source:
      path: "$PROVIDERS_DIR/datastore/sqlite"
  config:
    path: "$DEV_STATE_DIR/gestalt.db"
secrets:
  provider: env
server:
  public:
    port: $API_PORT
  encryption_key: "$(cat "$KEY_FILE")"
EOF
fi

if ! command -v go &>/dev/null; then
    err "Go is not installed. Install it from https://go.dev/dl/"
    exit 1
fi

if [[ -n "$CONFIG" ]]; then
    info "Config: $CONFIG"
fi
info "Building client UI export..."
npm run build

BIN_DIR="$GESTALTD_DIR/.dev-bin"
rm -rf "$BIN_DIR"
mkdir -p "$BIN_DIR"

info "Building Go server..."
(cd "$GESTALTD_DIR" && go build -o "$BIN_DIR/gestaltd" ./cmd/gestaltd)

info "Starting Go API server on port $API_PORT..."
warn "Dev mode - auth is disabled in the generated config."
warn "Client UI is served by gestaltd from $SCRIPT_DIR/out. Re-run this script after UI changes."
if [[ -n "$CONFIG" ]]; then
    (cd "$GESTALTD_DIR" && GESTALTD_CLIENT_UI_DIR="$SCRIPT_DIR/out" "$BIN_DIR/gestaltd" --config "$CONFIG") &
else
    (cd "$GESTALTD_DIR" && GESTALTD_CLIENT_UI_DIR="$SCRIPT_DIR/out" "$BIN_DIR/gestaltd") &
fi
API_PID=$!

API_READY=false
for i in $(seq 1 30); do
    if curl -sf "http://localhost:$API_PORT/health" >/dev/null 2>&1; then
        ok "Go server ready on port $API_PORT"
        API_READY=true
        break
    fi
    if ! kill -0 "$API_PID" 2>/dev/null; then
        err "Go server exited unexpectedly.${CONFIG:+ Check your config: $CONFIG}"
        wait "$API_PID" || true
        exit 1
    fi
    sleep 0.5
done
if [[ "$API_READY" != "true" ]]; then
    err "Go server did not become ready within 15 seconds"
    exit 1
fi

info "API: http://localhost:$API_PORT"
ok "Ready: http://localhost:$API_PORT"
echo ""
wait
