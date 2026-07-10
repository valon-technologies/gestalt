package appregistry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRegistryReader_FetchAppIndex(t *testing.T) {
	t.Parallel()

	indexJSON := `{
  "schemaVersion": 1,
  "apps": {
    "g-issues": {
      "displayName": "g-issues",
      "versions": {
        "0.0.1": {
          "metadata": "apps/g-issues/versions/0.0.1.json",
          "platforms": ["linux/amd64"],
          "publishedAt": "2026-07-10T02:00:00Z"
        }
      }
    }
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/g-issues/index.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(indexJSON))
	}))
	defer srv.Close()

	reader := &RegistryReader{HTTPClient: srv.Client()}
	index, err := reader.FetchAppIndex(t.Context(), srv.URL, "g-issues")
	if err != nil {
		t.Fatalf("FetchAppIndex: %v", err)
	}
	versions := VersionsFromIndex(index, "g-issues")
	if len(versions) != 1 || versions[0].Version != "0.0.1" {
		t.Fatalf("versions = %#v", versions)
	}
}

func TestVersionsFromIndex_SortsNewestFirst(t *testing.T) {
	t.Parallel()

	index := &Index{
		SchemaVersion: IndexSchemaVersion,
		Apps: map[string]AppVersions{
			"g-issues": {
				Versions: map[string]IndexVersion{
					"0.0.1": {
						Metadata:    "apps/g-issues/versions/0.0.1.json",
						PublishedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
					},
					"0.0.2": {
						Metadata:    "apps/g-issues/versions/0.0.2.json",
						PublishedAt: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
					},
				},
			},
		},
	}
	versions := VersionsFromIndex(index, "g-issues")
	if len(versions) != 2 || versions[0].Version != "0.0.2" {
		t.Fatalf("versions = %#v, want 0.0.2 first", versions)
	}
}

func TestRegistryReader_FetchAppIndex_NotFoundReturnsEmpty(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	reader := &RegistryReader{HTTPClient: srv.Client()}
	index, err := reader.FetchAppIndex(t.Context(), srv.URL, "missing-app")
	if err != nil {
		t.Fatalf("FetchAppIndex: %v", err)
	}
	if index == nil || len(index.Apps) != 0 {
		t.Fatalf("index = %#v, want empty index", index)
	}
}

func TestRegistryReader_FetchAppIndex_RejectsInvalidAppName(t *testing.T) {
	t.Parallel()

	reader := &RegistryReader{}
	_, err := reader.FetchAppIndex(t.Context(), "https://example.com", "..")
	if err == nil {
		t.Fatal("expected invalid app name error")
	}
}

func TestRegistryReader_FetchAppIndex_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	reader := &RegistryReader{HTTPClient: srv.Client()}
	_, err := reader.FetchAppIndex(t.Context(), srv.URL, "g-issues")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestVersionsFromIndex_RoundTripsPublishedAt(t *testing.T) {
	t.Parallel()

	raw := `{
  "schemaVersion": 1,
  "apps": {
    "g-issues": {
      "versions": {
        "0.0.1": {
          "metadata": "apps/g-issues/versions/0.0.1.json",
          "publishedAt": "2026-07-10T02:21:54.802491326Z"
        }
      }
    }
  }
}`
	index, err := DecodeIndex([]byte(raw))
	if err != nil {
		t.Fatalf("DecodeIndex: %v", err)
	}
	versions := VersionsFromIndex(index, "g-issues")
	if len(versions) != 1 {
		t.Fatalf("versions = %#v", versions)
	}
	got, err := json.Marshal(versions[0].PublishedAt)
	if err != nil {
		t.Fatalf("marshal publishedAt: %v", err)
	}
	if string(got) != `"2026-07-10T02:21:54.802491326Z"` {
		t.Fatalf("publishedAt json = %s", got)
	}
}
