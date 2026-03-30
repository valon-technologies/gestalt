package invocation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/core"
	coretesting "github.com/valon-technologies/gestalt/core/testing"
	"github.com/valon-technologies/gestalt/internal/invocation"
	"github.com/valon-technologies/gestalt/internal/principal"
	"github.com/valon-technologies/gestalt/internal/testutil"
)

func TestBrokerMalformedMetadataJSON_StructuredLog(t *testing.T) { //nolint:paralleltest // mutates slog.Default

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	prov := &stubProviderWithOps{
		StubIntegration: coretesting.StubIntegration{
			N:        "myservice",
			ConnMode: core.ConnectionModeUser,
			ExecuteFn: func(_ context.Context, _ string, _ map[string]any, _ string) (*core.OperationResult, error) {
				return &core.OperationResult{Status: http.StatusOK, Body: `{"ok":true}`}, nil
			},
		},
		ops: []core.Operation{{Name: "do_thing", Method: http.MethodGet}},
	}

	ds := &coretesting.StubDatastore{
		FindOrCreateUserFn: func(_ context.Context, email string) (*core.User, error) {
			return &core.User{ID: "u-1", Email: email}, nil
		},
		ListTokensForConnectionFn: func(_ context.Context, _, _, _ string) ([]*core.IntegrationToken, error) {
			return []*core.IntegrationToken{{
				UserID:       "u-1",
				Integration:  "myservice",
				Instance:     "default",
				AccessToken:  "tok-123",
				MetadataJSON: `{{{not json`,
			}}, nil
		},
	}

	broker := invocation.NewBroker(testutil.NewProviderRegistry(t, prov), ds)
	p := &principal.Principal{
		Identity: &core.UserIdentity{Email: "test@example.com"},
	}

	result, err := broker.Invoke(context.Background(), p, "myservice", "", "do_thing", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("expected 200, got %d", result.Status)
	}

	output := buf.String()
	if output == "" {
		t.Fatal("expected structured log output for malformed MetadataJSON, got empty")
	}

	var foundWarning bool
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not valid JSON: %q: %v", line, err)
		}

		if record["msg"] == "malformed metadata JSON" {
			foundWarning = true
			if record["level"] != "WARN" {
				t.Errorf("expected level=WARN, got level=%v", record["level"])
			}
			if record["provider"] != "myservice" {
				t.Errorf("expected provider=myservice, got provider=%v", record["provider"])
			}
			if _, ok := record["error"]; !ok {
				t.Error("malformed metadata JSON log missing 'error' field")
			}
		}
	}

	if !foundWarning {
		t.Errorf("did not find 'malformed metadata JSON' warning in output:\n%s", output)
	}
}
