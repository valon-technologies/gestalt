package server_test

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
)

func TestMountedUIPublicConfig(t *testing.T) {
	t.Parallel()
	for _, mount := range []string{"/", "/console"} {
		for _, dev := range []bool{false, true} {
			t.Run(mount+map[bool]string{false: "/static", true: "/dev"}[dev], func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				writeTestUIAsset(t, filepath.Join(root, "index.html"), "shell")
				public := map[string]any{"supportMessage": "Contact your workspace administrator."}
				ts := newTestServer(t, func(cfg *server.Config) {
					if dev {
						cfg.MountedUIs = []server.MountedUI{{
							Name: "console", AppName: "console", Path: mount, IsDev: true,
							PublicConfig: public,
							Handler:      http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("dev shell")) }),
						}}
					} else {
						cfg.AppDefs = map[string]*config.ProviderEntry{"console": {
							Static:             &config.AppStaticConfig{Mount: mount, Public: true, PublicConfig: public},
							ResolvedStaticRoot: root,
						}}
					}
				})
				testutil.CloseOnCleanup(t, ts)
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
					req, _ := http.NewRequest(method, ts.URL+strings.TrimRight(mount, "/")+"/public-config.json", nil)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodPost {
						if resp.StatusCode != http.StatusMethodNotAllowed {
							t.Fatalf("POST: %d", resp.StatusCode)
						}
						continue
					}
					if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
						t.Fatalf("response: %d %v", resp.StatusCode, resp.Header)
					}
					if method == http.MethodGet && string(body) != `{"supportMessage":"Contact your workspace administrator."}` {
						t.Fatalf("config: %s", body)
					}
					if method == http.MethodHead && len(body) != 0 {
						t.Fatalf("HEAD body: %s", body)
					}
				}
			})
		}
	}
}
