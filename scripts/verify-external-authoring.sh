#!/bin/sh
set -eu

GESTALTD="${1:-}"
SDK_VERSION="${SDK_VERSION:-}"
BUILT_BINARY=""

if [ -z "$SDK_VERSION" ]; then
    echo "error: SDK_VERSION env var is required (e.g. SDK_VERSION=0.0.0-alpha.1)" >&2
    exit 1
fi

if [ -z "$GESTALTD" ]; then
    echo "building gestaltd..."
    BUILT_BINARY=$(mktemp -u)
    ROOT="$(cd "$(dirname "$0")/.." && pwd)"
    (cd "$ROOT/server" && go build -o "$BUILT_BINARY" ./cmd/gestaltd)
    GESTALTD="$BUILT_BINARY"
fi

GESTALTD=$(cd "$(dirname "$GESTALTD")" && pwd)/$(basename "$GESTALTD")

WORK=$(mktemp -d)
SERVER_PID=""
trap 'if [ -n "$SERVER_PID" ]; then kill "$SERVER_PID" 2>/dev/null || true; fi; rm -rf "$WORK"; if [ -n "$BUILT_BINARY" ]; then rm -f "$BUILT_BINARY"; fi' EXIT

PORT=19300

echo "=== creating external plugin module ==="
cd "$WORK"
go mod init external-test-plugin

GOPRIVATE=github.com/valon-technologies/gestalt \
  go get "github.com/valon-technologies/gestalt/sdk/pluginsdk@v$SDK_VERSION"

cat > main.go <<'GO'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	pluginsdk "github.com/valon-technologies/gestalt/sdk/pluginsdk"
)

type provider struct{ prefix string }

func (p *provider) Name() string                                    { return "external" }
func (p *provider) DisplayName() string                             { return "External Test" }
func (p *provider) Description() string                             { return "" }
func (p *provider) ConnectionMode() pluginsdk.ConnectionMode        { return pluginsdk.ConnectionModeNone }
func (p *provider) Start(_ context.Context, _ string, cfg map[string]any) error {
	if v, ok := cfg["prefix"].(string); ok {
		p.prefix = v
	}
	return nil
}

func (p *provider) ListOperations() []pluginsdk.Operation {
	return []pluginsdk.Operation{
		{Name: "ping", Description: "Return pong", Method: "GET"},
	}
}

func (p *provider) Execute(_ context.Context, op string, _ map[string]any, _ string) (*pluginsdk.OperationResult, error) {
	if op == "ping" {
		body, _ := json.Marshal(map[string]string{"pong": p.prefix + "ok"})
		return &pluginsdk.OperationResult{Status: 200, Body: string(body)}, nil
	}
	return &pluginsdk.OperationResult{Status: 404, Body: `{"error":"not found"}`}, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := pluginsdk.ServeProvider(ctx, &provider{}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
GO

echo "=== building plugin ==="
go mod tidy
go build -o ./provider .

echo "=== packaging ==="
"$GESTALTD" plugin package --binary ./provider --id test/external --output ./plugin.tar.gz

echo "=== writing config ==="
cat > config.yaml <<CFG
auth:
  provider: none
datastore:
  provider: sqlite
  config:
    path: $WORK/gestalt.db
secrets:
  provider: env
server:
  port: $PORT
  encryption_key: verify-external-key
integrations:
  external:
    connection_mode: none
    plugin:
      package: $WORK/plugin.tar.gz
      config:
        prefix: "verified:"
CFG

echo "=== bundling config ==="
"$GESTALTD" bundle --config "$WORK/config.yaml" --output "$WORK/bundle"

echo "=== starting serve --locked ==="
"$GESTALTD" serve --locked --config "$WORK/bundle/config.yaml" &
SERVER_PID=$!

echo "=== polling health ==="
TRIES=0
while [ $TRIES -lt 30 ]; do
    if curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1; then
        echo "healthy after ${TRIES}s"
        break
    fi
    TRIES=$((TRIES + 1))
    sleep 1
done

if [ $TRIES -ge 30 ]; then
    echo "FAIL: server did not become healthy"
    exit 1
fi

echo "=== waiting for ready ==="
TRIES=0
while [ $TRIES -lt 30 ]; do
    if curl -sf "http://localhost:$PORT/ready" >/dev/null 2>&1; then
        echo "ready after ${TRIES}s"
        break
    fi
    TRIES=$((TRIES + 1))
    sleep 1
done

if [ $TRIES -ge 30 ]; then
    echo "FAIL: server did not become ready"
    exit 1
fi

echo "=== invoking plugin ==="
RESPONSE=$(curl -s -X GET "http://localhost:$PORT/api/v1/external/ping") || true

echo "response: $RESPONSE"

if echo "$RESPONSE" | grep -q '"verified:ok"'; then
    echo ""
    echo "PASS: external plugin authoring workflow verified"
else
    echo "FAIL: unexpected response"
    exit 1
fi
