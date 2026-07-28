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

func TestRegistryReader_FetchAppIndexConditional(t *testing.T) {
	t.Parallel()

	const indexJSON = `{
  "schemaVersion": 1,
  "apps": {
    "g-issues": {
      "versions": {
        "0.0.1": {
          "metadata": "apps/g-issues/versions/0.0.1.json",
          "publishedAt": "2026-07-10T02:00:00Z"
        }
      }
    }
  }
}`

	t.Run("not modified", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("If-None-Match"); got != `W/"current"` {
				t.Errorf("If-None-Match = %q, want weak ETag", got)
			}
			w.WriteHeader(http.StatusNotModified)
		}))
		defer srv.Close()

		result, err := (&RegistryReader{HTTPClient: srv.Client()}).
			FetchAppIndexConditional(t.Context(), srv.URL, "g-issues", `W/"current"`)
		if err != nil {
			t.Fatalf("FetchAppIndexConditional: %v", err)
		}
		if !result.NotModified || result.Index != nil || result.ETag != "" {
			t.Fatalf("result = %#v, want not modified", result)
		}
	})

	t.Run("modified with etag", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("If-None-Match"); got != "" {
				t.Errorf("If-None-Match = %q, want empty", got)
			}
			w.Header().Set("ETag", `"next"`)
			_, _ = w.Write([]byte(indexJSON))
		}))
		defer srv.Close()

		result, err := (&RegistryReader{HTTPClient: srv.Client()}).
			FetchAppIndexConditional(t.Context(), srv.URL, "g-issues", "")
		if err != nil {
			t.Fatalf("FetchAppIndexConditional: %v", err)
		}
		if result.NotModified || result.ETag != `"next"` {
			t.Fatalf("result = %#v", result)
		}
		versions := VersionsFromIndex(result.Index, "g-issues")
		if len(versions) != 1 || versions[0].Version != "0.0.1" {
			t.Fatalf("versions = %#v", versions)
		}
	})

	t.Run("modified without etag", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(indexJSON))
		}))
		defer srv.Close()

		result, err := (&RegistryReader{HTTPClient: srv.Client()}).
			FetchAppIndexConditional(t.Context(), srv.URL, "g-issues", `"old"`)
		if err != nil {
			t.Fatalf("FetchAppIndexConditional: %v", err)
		}
		if result.NotModified || result.ETag != "" || result.Index == nil {
			t.Fatalf("result = %#v", result)
		}
	})

	t.Run("invalid json does not expose etag", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("ETag", `"bad"`)
			_, _ = w.Write([]byte(`{`))
		}))
		defer srv.Close()

		result, err := (&RegistryReader{HTTPClient: srv.Client()}).
			FetchAppIndexConditional(t.Context(), srv.URL, "g-issues", "")
		if err == nil || result != nil {
			t.Fatalf("result, error = (%#v, %v), want decode error", result, err)
		}
	})
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

func TestRegistryReader_FetchPendingIndex(t *testing.T) {
	t.Parallel()

	pendingJSON := `{
  "schemaVersion": 1,
  "app": "g-issues",
  "pending": {
    "0.0.2": {
      "version": "0.0.2",
      "sourceRef": "abc123def456abc123def456abc123def456abcd",
      "startedAt": "2026-07-24T19:00:00Z",
      "updatedAt": "2026-07-24T19:04:12Z",
      "phase": "publishing"
    }
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps/g-issues/pending.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(pendingJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reader := &RegistryReader{HTTPClient: srv.Client()}
	index, err := reader.FetchPendingIndex(t.Context(), srv.URL, "g-issues")
	if err != nil {
		t.Fatalf("FetchPendingIndex: %v", err)
	}
	if len(index.Pending) != 1 || index.Pending["0.0.2"].Phase != PendingPhasePublishing {
		t.Fatalf("pending = %#v", index.Pending)
	}
}

func TestRegistryReader_FetchFailedIndex_NotFoundReturnsEmpty(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	reader := &RegistryReader{HTTPClient: srv.Client()}
	index, err := reader.FetchFailedIndex(t.Context(), srv.URL, "g-issues")
	if err != nil {
		t.Fatalf("FetchFailedIndex: %v", err)
	}
	if index == nil || len(index.Failed) != 0 {
		t.Fatalf("failed = %#v, want empty catalog", index)
	}
}
