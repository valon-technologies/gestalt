package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

func TestAdvertisedConnectionsZipSchemaAndStatus(t *testing.T) {
	t.Parallel()

	s := &Server{}
	app := &config.ProviderEntry{
		Connections: map[string]*config.ConnectionDef{
			"workspace": {
				ConnectionID: "demo:workspace",
				DisplayName:  "Workspace",
				Mode:         providermanifestv1.ConnectionModeSubject,
				Auth: config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeManual,
				},
			},
			"webhook": {
				ConnectionID: "demo:webhook",
				DisplayName:  "Webhook",
				Mode:         providermanifestv1.ConnectionModeNone,
			},
		},
	}
	advertised := s.advertisedConnectionsForPlugin("demo", app)
	schemas := s.connectionSchemasFromAdvertised("demo", advertised)
	infos := s.connectionInfosFromAdvertised(context.Background(), "demo", advertised, nil, nil)
	if len(schemas) == 0 {
		t.Fatal("expected advertised connection schema")
	}
	if len(schemas) != len(infos) {
		t.Fatalf("schema count %d != status count %d", len(schemas), len(infos))
	}
	for i := range schemas {
		if schemas[i].Name != infos[i].Name {
			t.Fatalf("name[%d] schema %q status %q", i, schemas[i].Name, infos[i].Name)
		}
		if schemas[i].Mode != infos[i].Mode {
			t.Fatalf("mode[%d] schema %q status %q", i, schemas[i].Mode, infos[i].Mode)
		}
	}
}

func TestProjectComposedListingUsesDirectoryAdvertisedConnections(t *testing.T) {
	t.Parallel()

	s := &Server{}
	dir := &appDirectory{entries: []appDirectoryEntry{{
		Name: "demo",
		Advertised: []advertisedConnection{{
			Name:               "workspace",
			InstanceConnection: "workspace",
			Def: config.ConnectionDef{
				ConnectionID: "demo:workspace",
				Mode:         providermanifestv1.ConnectionModeSubject,
				Auth: config.ConnectionAuthDef{
					Type: providermanifestv1.AuthTypeManual,
				},
			},
			IncludeWithoutAuth: true,
		}},
	}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
	out, err := s.projectComposedAppListing(req, dir)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(out) != 1 || len(out[0].Connections) != 1 || out[0].Connections[0].Name != "workspace" {
		t.Fatalf("composed connections = %+v, want workspace from directory advertised list", out)
	}
}

func TestWriteAppDirectoryErrorUsesIconFallback(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/apps/demo/icon", nil)
	writeAppDirectoryError(rr, req, errors.New("boom"), "failed to load app icon")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Error != "failed to load app icon" {
		t.Fatalf("error = %q, want failed to load app icon", payload.Error)
	}
}
