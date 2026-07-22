package appregistry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/appregistry/registrytest"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	"github.com/valon-technologies/gestalt/server/services/apps/providerpkg"
)

const slackSourceAddress = "github.com/valon-technologies/valon-tools/apps/slack"

func assertInstallValidationReason(t *testing.T, err error, want appregistry.InstallValidationReason) {
	t.Helper()
	if !errors.Is(err, appregistry.ErrInstallValidationFailed) {
		t.Fatalf("Validate = %v, want ErrInstallValidationFailed", err)
	}
	reason, ok := appregistry.InstallValidationReasonFrom(err)
	if !ok {
		t.Fatalf("InstallValidationReasonFrom(%v) = false", err)
	}
	if reason != want {
		t.Fatalf("reason = %q, want %q (err = %v)", reason, want, err)
	}
}

func TestInstallValidator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	platform := providerpkg.CurrentPlatformString()

	baseEntry := func(mutate func(*appregistry.Entry)) appregistry.Entry {
		entry := appregistry.Entry{
			SchemaVersion: appregistry.EntrySchemaVersion,
			App:           "g-issues",
			Version:       "1.0.0",
			SourceRef:     "abc123def456abc123def456abc123def456abcd",
			ManifestPath:  "valon-tools/apps/g-issues/manifest.yaml",
			Repository:    "github.com/valon-technologies/valon-tools",
			Artifacts: map[string]appregistry.Artifact{
				platform: {
					URL:       "https://example.com/artifact.tar.gz",
					PublicURL: "https://example.com/artifact.tar.gz",
					SHA256:    "deadbeef",
				},
			},
			PublishedAt: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		}
		if mutate != nil {
			mutate(&entry)
		}
		return entry
	}

	slackEntry := func(mutate func(*appregistry.Entry)) appregistry.Entry {
		entry := appregistry.Entry{
			SchemaVersion: appregistry.EntrySchemaVersion,
			App:           "slack",
			Version:       "2.0.0",
			SourceRef:     "abc123def456abc123def456abc123def456abcd",
			ManifestPath:  "valon-tools/apps/slack/manifest.yaml",
			Repository:    "github.com/valon-technologies/valon-tools",
			Artifacts: map[string]appregistry.Artifact{
				platform: {
					URL:       "https://example.com/slack.tar.gz",
					PublicURL: "https://example.com/slack.tar.gz",
					SHA256:    "cafebabe",
				},
			},
			Interface: appregistry.Interface{
				Operations: map[string]appregistry.OperationContract{
					"channels.list": {InputSchema: json.RawMessage(`{"type":"object"}`)},
				},
			},
			PublishedAt: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		}
		if mutate != nil {
			mutate(&entry)
		}
		return entry
	}

	newValidator := func(t *testing.T, svc *testutil.Services, entries map[string]appregistry.Entry, gestaltdVersion string) *appregistry.InstallValidator {
		t.Helper()
		registry, err := config.NewGCSAppRegistry(registrytest.Bucket)
		if err != nil {
			t.Fatalf("NewGCSAppRegistry: %v", err)
		}
		encoded := make(map[string][]byte, len(entries))
		for version, entry := range entries {
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatalf("Marshal %s: %v", version, err)
			}
			encoded[version] = data
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for version, data := range encoded {
				if r.URL.Path == "/"+registrytest.Bucket+"/apps/"+entries[version].App+"/versions/"+version+".json" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(data)
					return
				}
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(srv.Close)
		return &appregistry.InstallValidator{
			Registries: map[string]config.AppRegistryConfig{
				"toolshed": registry,
			},
			Reader:          registrytest.NewReaderForServer(t, srv.URL),
			ChangeRequests:  svc.AppVersionChangeRequests,
			GestaltdVersion: gestaltdVersion,
			Platform:        platform,
		}
	}

	installFleetApp := func(t *testing.T, svc *testutil.Services, app, version string) {
		t.Helper()
		if _, err := svc.AppVersionChangeRequests.AppendRequest(ctx, &core.AppVersionChangeRequest{
			App: app, FromVersion: "registry:first-install", ToVersion: version, Timestamp: time.Now().UTC(),
			Metadata: map[string]any{"registry": "toolshed"},
		}); err != nil {
			t.Fatalf("AppendRequest %s: %v", app, err)
		}
	}

	t.Run("accepts_dependencies", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name         string
			slackVersion string
			depKey       string
			withOps      bool
		}{
			{name: "release_with_operations", slackVersion: "2.0.0", depKey: "slack", withOps: true},
			{name: "snapshot_version", slackVersion: "2.0.0-snapshot.gabc123", depKey: "slack", withOps: false},
			{name: "source_address_key", slackVersion: "2.0.0", depKey: slackSourceAddress, withOps: false},
		}
		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				svc := testutil.NewStubServices(t)
				installFleetApp(t, svc, "slack", tc.slackVersion)
				validator := newValidator(t, svc, map[string]appregistry.Entry{
					tc.slackVersion: slackEntry(func(e *appregistry.Entry) {
						e.Version = tc.slackVersion
					}),
				}, "1.0.0")
				req := appregistry.AppRequirement{Version: "^2.0.0"}
				if tc.withOps {
					req.Operations = map[string]appregistry.OperationRequirement{
						"channels.list": {InputSchemaHash: appregistry.InputSchemaHash(json.RawMessage(`{"type":"object"}`))},
					}
				}
				entry := baseEntry(func(e *appregistry.Entry) {
					e.Requires = appregistry.Requires{Apps: map[string]appregistry.AppRequirement{tc.depKey: req}}
				})
				if err := validator.Validate(ctx, appregistry.ValidateInput{
					Registry: "toolshed",
					App:      "g-issues",
					Version:  "1.0.0",
					Entry:    &entry,
				}); err != nil {
					t.Fatalf("Validate: %v", err)
				}
			})
		}
	})

	t.Run("rejects_platform_artifact", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			mutate     func(*appregistry.Entry)
			wantDetail string
		}{
			{
				name: "missing_host_platform",
				mutate: func(e *appregistry.Entry) {
					e.Artifacts = map[string]appregistry.Artifact{
						"plan9/386": {URL: "https://example.com/x", PublicURL: "https://example.com/x", SHA256: "abc"},
					}
				},
			},
			{
				name: "incomplete_sha256",
				mutate: func(e *appregistry.Entry) {
					artifact := e.Artifacts[platform]
					artifact.SHA256 = ""
					e.Artifacts[platform] = artifact
				},
				wantDetail: "missing sha256",
			},
		}
		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				svc := testutil.NewStubServices(t)
				validator := newValidator(t, svc, nil, "1.0.0")
				entry := baseEntry(tc.mutate)
				err := validator.Validate(ctx, appregistry.ValidateInput{
					Registry: "toolshed", App: "g-issues", Version: "1.0.0", Entry: &entry,
				})
				assertInstallValidationReason(t, err, appregistry.InstallValidationPlatformArtifactMissing)
				if tc.wantDetail != "" && !strings.Contains(err.Error(), tc.wantDetail) {
					t.Fatalf("Validate = %v, want detail %q", err, tc.wantDetail)
				}
			})
		}
	})

	t.Run("gestaltd_version", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name            string
			gestaltdVersion string
			minGestaltd     string
			wantReason      appregistry.InstallValidationReason
		}{
			{name: "incompatible", gestaltdVersion: "0.6.0", minGestaltd: "0.7.0", wantReason: appregistry.InstallValidationGestaltdVersionIncompatible},
			{name: "ci_stamp_skipped", gestaltdVersion: "0.0.0-ci+gabc123def", minGestaltd: "99.0.0"},
		}
		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				svc := testutil.NewStubServices(t)
				validator := newValidator(t, svc, nil, tc.gestaltdVersion)
				entry := baseEntry(func(e *appregistry.Entry) {
					e.Compatibility = appregistry.Compatibility{MinGestaltdVersion: tc.minGestaltd}
				})
				err := validator.Validate(ctx, appregistry.ValidateInput{
					Registry: "toolshed", App: "g-issues", Version: "1.0.0", Entry: &entry,
				})
				if tc.wantReason == "" {
					if err != nil {
						t.Fatalf("Validate: %v", err)
					}
					return
				}
				assertInstallValidationReason(t, err, tc.wantReason)
			})
		}
	})

	t.Run("rejects_unsatisfied_dependency", func(t *testing.T) {
		t.Parallel()
		svc := testutil.NewStubServices(t)
		installFleetApp(t, svc, "slack", "1.4.0")
		validator := newValidator(t, svc, map[string]appregistry.Entry{
			"1.4.0": slackEntry(func(e *appregistry.Entry) {
				e.Version = "1.4.0"
				e.Interface = appregistry.Interface{}
			}),
		}, "1.0.0")

		tests := []struct {
			name       string
			setup      func() (*appregistry.InstallValidator, appregistry.Entry)
			wantReason appregistry.InstallValidationReason
		}{
			{
				name: "missing_dependency_app",
				setup: func() (*appregistry.InstallValidator, appregistry.Entry) {
					entry := baseEntry(func(e *appregistry.Entry) {
						e.Requires = appregistry.Requires{Apps: map[string]appregistry.AppRequirement{
							"slack": {Version: "^2.0.0"},
						}}
					})
					return newValidator(t, testutil.NewStubServices(t), nil, "1.0.0"), entry
				},
				wantReason: appregistry.InstallValidationDependencyNotInstalled,
			},
			{
				name: "version_outside_range",
				setup: func() (*appregistry.InstallValidator, appregistry.Entry) {
					entry := baseEntry(func(e *appregistry.Entry) {
						e.Requires = appregistry.Requires{Apps: map[string]appregistry.AppRequirement{
							"slack": {Version: "^2.0.0"},
						}}
					})
					return validator, entry
				},
				wantReason: appregistry.InstallValidationDependencyVersionUnsatisfied,
			},
			{
				name: "missing_required_operation",
				setup: func() (*appregistry.InstallValidator, appregistry.Entry) {
					entry := baseEntry(func(e *appregistry.Entry) {
						e.Requires = appregistry.Requires{Apps: map[string]appregistry.AppRequirement{
							"slack": {
								Version: "^1.0.0",
								Operations: map[string]appregistry.OperationRequirement{
									"channels.list": {},
								},
							},
						}}
					})
					return validator, entry
				},
				wantReason: appregistry.InstallValidationDependencyOperationMissing,
			},
		}
		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				validator, entry := tc.setup()
				err := validator.Validate(ctx, appregistry.ValidateInput{
					Registry: "toolshed", App: "g-issues", Version: "1.0.0", Entry: &entry,
				})
				assertInstallValidationReason(t, err, tc.wantReason)
			})
		}
	})

	t.Run("reverse_dependents", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name         string
			depKey       string
			depVersion   string
			depOps       map[string]appregistry.OperationRequirement
			candidateVer string
			wantReason   appregistry.InstallValidationReason
		}{
			{
				name:         "operation_missing",
				depOps:       map[string]appregistry.OperationRequirement{"postMessage": {}},
				candidateVer: "2.0.0",
				wantReason:   appregistry.InstallValidationReverseDependentOperationMissing,
			},
			{
				name:         "version_outside_range",
				depKey:       "slack",
				depVersion:   "^2.0.0",
				candidateVer: "3.0.0",
				wantReason:   appregistry.InstallValidationReverseDependentVersionUnsatisfied,
			},
			{
				name:         "version_outside_range_source_address_key",
				depKey:       slackSourceAddress,
				depVersion:   "^2.0.0",
				candidateVer: "3.0.0",
				wantReason:   appregistry.InstallValidationReverseDependentVersionUnsatisfied,
			},
			{
				name:         "snapshot_version_accepted",
				depKey:       "slack",
				depVersion:   "^2.0.0",
				candidateVer: "2.0.0-snapshot.gabc123",
			},
		}
		for _, tc := range tests {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				svc := testutil.NewStubServices(t)
				installFleetApp(t, svc, "g-issues", "1.0.0")
				if tc.depKey == "" {
					tc.depKey = "slack"
				}
				validator := newValidator(t, svc, map[string]appregistry.Entry{
					"1.0.0": baseEntry(func(e *appregistry.Entry) {
						req := appregistry.AppRequirement{Version: tc.depVersion, Operations: tc.depOps}
						e.Requires = appregistry.Requires{Apps: map[string]appregistry.AppRequirement{tc.depKey: req}}
					}),
				}, "1.0.0")
				candidate := slackEntry(func(e *appregistry.Entry) {
					e.Version = tc.candidateVer
					if tc.wantReason == appregistry.InstallValidationReverseDependentOperationMissing {
						e.Interface = appregistry.Interface{Operations: map[string]appregistry.OperationContract{}}
					}
				})
				err := validator.Validate(ctx, appregistry.ValidateInput{
					Registry: "toolshed",
					App:      "slack",
					Version:  tc.candidateVer,
					Entry:    &candidate,
				})
				if tc.wantReason == "" {
					if err != nil {
						t.Fatalf("Validate: %v", err)
					}
					return
				}
				assertInstallValidationReason(t, err, tc.wantReason)
			})
		}
	})

	t.Run("reverse_dependent_infra", func(t *testing.T) {
		t.Parallel()

		t.Run("ignores_unrelated_missing_metadata", func(t *testing.T) {
			t.Parallel()
			svc := testutil.NewStubServices(t)
			installFleetApp(t, svc, "orphan", "1.0.0")
			validator := newValidator(t, svc, nil, "1.0.0")
			candidate := slackEntry(nil)
			if err := validator.Validate(ctx, appregistry.ValidateInput{
				Registry: "toolshed", App: "slack", Version: "2.0.0", Entry: &candidate,
			}); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})

		t.Run("registry_not_configured", func(t *testing.T) {
			t.Parallel()
			svc := testutil.NewStubServices(t)
			installFleetApp(t, svc, "g-issues", "1.0.0")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			}))
			t.Cleanup(srv.Close)
			validator := &appregistry.InstallValidator{
				Registries:      map[string]config.AppRegistryConfig{},
				Reader:          registrytest.NewReaderForServer(t, srv.URL),
				ChangeRequests:  svc.AppVersionChangeRequests,
				GestaltdVersion: "1.0.0",
				Platform:        platform,
			}
			candidate := slackEntry(nil)
			err := validator.Validate(ctx, appregistry.ValidateInput{
				Registry: "toolshed", App: "slack", Version: "2.0.0", Entry: &candidate,
			})
			if !errors.Is(err, appregistry.ErrAppRegistryNotConfigured) {
				t.Fatalf("Validate = %v, want ErrAppRegistryNotConfigured", err)
			}
		})
	})

	t.Run("workflow_targets", func(t *testing.T) {
		t.Parallel()

		configApps := map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
			"slack":    {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		workflowValidator := func(t *testing.T, svc *testutil.Services, entries map[string]appregistry.Entry) *appregistry.InstallValidator {
			t.Helper()
			validator := newValidator(t, svc, entries, "1.0.0")
			validator.ConfigApps = configApps
			return validator
		}

		t.Run("rejects_missing_target_app", func(t *testing.T) {
			t.Parallel()
			svc := testutil.NewStubServices(t)
			validator := workflowValidator(t, svc, nil)
			entry := baseEntry(func(e *appregistry.Entry) {
				e.Workflows = appregistry.Workflows{
					Definitions: []appregistry.WorkflowDefinitionRef{{
						ID: "slack_v2_smoke_test",
						Steps: []appregistry.WorkflowAppCallRef{{
							App:       "gIssues",
							Operation: "handle_slack_event",
						}},
					}},
				}
			})
			err := validator.Validate(ctx, appregistry.ValidateInput{
				Registry: "toolshed", App: "g-issues", Version: "1.0.0", Entry: &entry,
			})
			assertInstallValidationReason(t, err, appregistry.InstallValidationWorkflowTargetAppMissing)
		})

		t.Run("accepts_configured_self_target", func(t *testing.T) {
			t.Parallel()
			svc := testutil.NewStubServices(t)
			validator := workflowValidator(t, svc, nil)
			entry := baseEntry(func(e *appregistry.Entry) {
				e.Workflows = appregistry.Workflows{
					Definitions: []appregistry.WorkflowDefinitionRef{{
						ID: "slack_v2_smoke_test",
						Steps: []appregistry.WorkflowAppCallRef{{
							App:       "g-issues",
							Operation: "handle_slack_event",
						}},
					}},
				}
			})
			if err := validator.Validate(ctx, appregistry.ValidateInput{
				Registry: "toolshed", App: "g-issues", Version: "1.0.0", Entry: &entry,
			}); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})

		t.Run("rejects_missing_target_operation", func(t *testing.T) {
			t.Parallel()
			svc := testutil.NewStubServices(t)
			installFleetApp(t, svc, "slack", "2.0.0")
			validator := workflowValidator(t, svc, map[string]appregistry.Entry{
				"2.0.0": slackEntry(nil),
			})
			entry := baseEntry(func(e *appregistry.Entry) {
				e.Workflows = appregistry.Workflows{
					Definitions: []appregistry.WorkflowDefinitionRef{{
						ID: "notify",
						Steps: []appregistry.WorkflowAppCallRef{{
							App:       "slack",
							Operation: "postMessage",
						}},
					}},
				}
			})
			err := validator.Validate(ctx, appregistry.ValidateInput{
				Registry: "toolshed", App: "g-issues", Version: "1.0.0", Entry: &entry,
			})
			assertInstallValidationReason(t, err, appregistry.InstallValidationWorkflowTargetOperationMissing)
		})
	})
}

func TestInstaller_validation_failure_writes_nothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := testutil.NewStubServices(t)
	fixture := registrytest.NewInstallFixture(t)

	publicURL, err := fixture.Registry.PublicURL()
	if err != nil {
		t.Fatalf("PublicURL: %v", err)
	}
	entry, err := fixture.Reader.FetchEntry(ctx, publicURL, "g-issues", fixture.Version)
	if err != nil {
		t.Fatalf("FetchEntry: %v", err)
	}
	entry.Compatibility = appregistry.Compatibility{MinGestaltdVersion: "99.0.0"}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + registrytest.Bucket + "/apps/g-issues/versions/" + fixture.Version + ".json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(entryJSON)
		case "/" + registrytest.Bucket + "/apps/g-issues/artifacts/" + fixture.Version + "/artifact.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(fixture.ArchiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(registrySrv.Close)

	installer := &appregistry.Installer{
		Registries: map[string]config.AppRegistryConfig{
			"toolshed": fixture.Registry,
		},
		ConfigApps: map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		},
		Reader:          registrytest.NewReaderForServer(t, registrySrv.URL),
		ChangeRequests:  svc.AppVersionChangeRequests,
		Locks:           svc.AppVersionInstallLocks,
		Rollouts:        svc.AppRollouts,
		GestaltdVersion: "0.1.0",
	}

	_, err = installer.Add(ctx, appregistry.InstallInput{
		Registry: "toolshed",
		App:      "g-issues",
		Version:  fixture.Version,
	})
	if !errors.Is(err, appregistry.ErrInstallValidationFailed) {
		t.Fatalf("Add = %v, want ErrInstallValidationFailed", err)
	}
	if rollout, getErr := svc.AppRollouts.Get(ctx, "g-issues"); getErr == nil && rollout != nil {
		t.Fatalf("rollout = %#v, want none", rollout)
	}
	requests, err := svc.AppVersionChangeRequests.ListRequestsByApp(ctx, "g-issues")
	if err != nil {
		t.Fatalf("ListRequestsByApp: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("requests = %#v", requests)
	}
}
