package server_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/providerrelease"
	"github.com/valon-technologies/gestalt/server/internal/server"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

func TestAppAdminRegistryPublishCreateReturnsUploadHeaders(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": harness.registry}
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	createBody, _ := json.Marshal(map[string]any{"declaration": harness.declaration})
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer alice-token")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("POST publish begin: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	var created struct {
		PublishID string `json:"publishId"`
		State     string `json:"state"`
		Uploads   []struct {
			Headers map[string]string `json:"headers"`
		} `json:"uploads"`
		Publisher string `json:"publisher"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.State != appregistry.PublishStateUploading || len(created.Uploads) != 1 || created.Uploads[0].Headers == nil {
		t.Fatalf("create = %#v", created)
	}
	if created.Uploads[0].Headers[appregistry.UploadHeaderXGoogMetaSHA256] == "" {
		t.Fatalf("headers = %#v", created.Uploads[0].Headers)
	}
	if created.Publisher != "" {
		t.Fatalf("publisher leaked in response: %q", created.Publisher)
	}
}

func TestAppAdminRegistryPublishFlow(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": harness.registry}
		cfg.AppRegistryReader = harness.reader
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	created := postPublishBegin(t, ts.URL, harness.declaration)
	if err := appregistry.ApplyMemoryUpload(harness.mem, created.Uploads[0].UploadURL, harness.artifactBytes, harness.declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}
	finalized := postPublishFinalize(t, ts.URL, created.PublishID, harness.declaration)
	if finalized.State != appregistry.PublishStatePublished {
		t.Fatalf("finalize = %#v", finalized)
	}
	resumed := postPublishBegin(t, ts.URL, harness.declaration)
	if resumed.State != appregistry.PublishStatePublished || len(resumed.Uploads) != 0 {
		t.Fatalf("resume = %#v", resumed)
	}
}

func TestAppAdminRegistryPublishConcurrentFinalizeMatching(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": harness.registry}
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	created := postPublishBegin(t, ts.URL, harness.declaration)
	if err := appregistry.ApplyMemoryUpload(harness.mem, created.Uploads[0].UploadURL, harness.artifactBytes, harness.declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	const workers = 5
	var wg sync.WaitGroup
	statuses := make(chan int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- postPublishFinalizeStatus(t, ts.URL, created.PublishID, harness.declaration)
		}()
	}
	wg.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("finalize status = %d, want 200", status)
		}
	}
}

func TestAppAdminRegistryPublishRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID("bob")
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("bob-token", subjectID, "")
		cfg.Authorization = &serverTestAuthorizationProvider{}
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": harness.registry}
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	body, _ := json.Marshal(map[string]any{"declaration": harness.declaration})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer bob-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST publish: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
}

func TestAppAdminRegistryPublishRejectsWrongWritableRegistry(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "other-registry"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{
			"other-registry": harness.registry,
			"toolshed":       harness.registry,
		}
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	body, _ := json.Marshal(map[string]any{"declaration": harness.declaration})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST publish: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s, want 404 for non-writable registry", resp.StatusCode, responseBody)
	}
}

func TestAppAdminRegistryPublishRejectsZeroArtifactSize(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": harness.registry}
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	declaration := *harness.declaration
	declaration.Artifacts[0].Size = 0
	body, _ := json.Marshal(map[string]any{"declaration": &declaration})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST publish: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s, want 400 for zero artifact size", resp.StatusCode, responseBody)
	}
}

func TestAppAdminRegistryPublishRejectsCrossAppFinalize(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
			testAuthorizationRelationship(subjectID, "admin", "app", "other-app"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues":  {Source: config.ProviderSource{Registry: "toolshed"}},
			"other-app": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": harness.registry}
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	created := postPublishBegin(t, ts.URL, harness.declaration)
	otherDecl := *harness.declaration
	otherDecl.Manifest = &providermanifestv1.Manifest{
		Kind: providermanifestv1.KindApp, Source: "github.com/valon-technologies/valon-tools/apps/other-app",
		Version: harness.declaration.Manifest.Version, Spec: &providermanifestv1.Spec{},
	}
	status := postPublishFinalizeStatusForApp(t, ts.URL, "other-app", created.PublishID, &otherDecl)
	if status != http.StatusBadRequest {
		t.Fatalf("cross-app finalize status = %d, want 400", status)
	}
}

func TestBootstrapAppRegistryPublishDisabledSkipsSigningReadiness(t *testing.T) {
	t.Parallel()

	service, err := server.BootstrapAppRegistryPublishForTest(&config.Config{})
	if err != nil {
		t.Fatalf("bootstrap disabled: %v", err)
	}
	if service != nil {
		t.Fatal("expected nil publish service when disabled")
	}
}

func TestBootstrapAppRegistryPublishFailsWhenSigningUnavailable(t *testing.T) {
	t.Parallel()

	registry, err := config.NewGCSAppRegistry("gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	cfg := &config.Config{
		AppRegistries: map[string]config.AppRegistryConfig{"toolshed": registry},
	}
	cfg.Server.AppRegistry.Publish.Enabled = true
	cfg.Server.AppRegistry.Publish.WritableRegistry = "toolshed"

	restorePermissions := stubCheckGCSRegistryPermissions(t, nil)
	defer restorePermissions()
	restoreSigning := stubCheckUploadSigning(t, fmt.Errorf("signBlob unavailable"))
	defer restoreSigning()

	_, err = server.BootstrapAppRegistryPublishForTest(cfg)
	if err == nil || !strings.Contains(err.Error(), "signBlob unavailable") {
		t.Fatalf("bootstrap error = %v", err)
	}
}

func TestBootstrapAppRegistryPublishFailsWhenObjectPermissionsUnavailable(t *testing.T) {
	t.Parallel()

	registry, err := config.NewGCSAppRegistry("gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	cfg := &config.Config{
		AppRegistries: map[string]config.AppRegistryConfig{"toolshed": registry},
	}
	cfg.Server.AppRegistry.Publish.Enabled = true
	cfg.Server.AppRegistry.Publish.WritableRegistry = "toolshed"

	restorePermissions := stubCheckGCSRegistryPermissions(t, errors.New("storage.objects.get denied"))
	defer restorePermissions()

	_, err = server.BootstrapAppRegistryPublishForTest(cfg)
	if err == nil || !strings.Contains(err.Error(), "storage.objects.get denied") {
		t.Fatalf("bootstrap error = %v", err)
	}
}

type registryPublishHarness struct {
	registry      config.AppRegistryConfig
	reader        *appregistry.RegistryReader
	service       *appregistry.StatelessPublishService
	mem           *appregistry.MemoryObjectStore
	declaration   *appregistry.PublishDeclaration
	artifactBytes []byte
}

func newRegistryPublishHarness(t *testing.T) registryPublishHarness {
	t.Helper()
	registry, err := config.NewGCSAppRegistry("gitlab-peach-street-gestalt-app-registry")
	if err != nil {
		t.Fatalf("NewGCSAppRegistry: %v", err)
	}
	store := appregistry.NewMemoryObjectStore()
	mem := store
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.PublishLimits{RequiredPlatforms: []string{"linux/amd64"}}
	service := &appregistry.StatelessPublishService{
		Registry: "toolshed", StorageRoot: "gs://gitlab-peach-street-gestalt-app-registry",
		PublicRoot: "https://storage.googleapis.com/gitlab-peach-street-gestalt-app-registry",
		Store:      store, Signer: signer, Writer: &appregistry.Writer{Store: store}, Limits: limits,
	}
	declaration, artifactBytes := testServerPublishDeclaration(t, "g-issues", "0.3.0-dev.server")
	return registryPublishHarness{
		registry:      registry,
		reader:        &appregistry.RegistryReader{},
		service:       service,
		mem:           mem,
		declaration:   declaration,
		artifactBytes: artifactBytes,
	}
}

func testServerPublishDeclaration(t *testing.T, appName, version string) (*appregistry.PublishDeclaration, []byte) {
	t.Helper()
	artifactBytes := []byte("server-artifact-" + version)
	sum := sha256.Sum256(artifactBytes)
	digest := hex.EncodeToString(sum[:])
	release := &providerrelease.Metadata{
		Schema: providerrelease.SchemaName, SchemaVersion: providerrelease.SchemaVersion,
		Package: "github.com/valon-technologies/valon-tools/apps/" + appName,
		Kind:    providermanifestv1.KindApp, Version: version, Runtime: providerrelease.RuntimeExecutable,
		Artifacts: providerrelease.Artifacts{"linux/amd64": {Path: "linux-amd64.tar.gz", SHA256: digest}},
		StaticValidation: &providerrelease.StaticValidation{
			Manifest: &providermanifestv1.Manifest{
				Kind: providermanifestv1.KindApp, Source: "github.com/valon-technologies/valon-tools/apps/" + appName,
				Version: version, Spec: &providermanifestv1.Spec{},
			},
			Catalog: &catalog.Catalog{Name: appName, Operations: []catalog.CatalogOperation{{ID: "echo", Method: "POST"}}},
		},
	}
	return &appregistry.PublishDeclaration{
		Schema: appregistry.PublishDeclarationSchemaVersion,
		Manifest: &providermanifestv1.Manifest{
			Kind: providermanifestv1.KindApp, Source: "github.com/valon-technologies/valon-tools/apps/" + appName,
			Version: version, Spec: &providermanifestv1.Spec{},
		},
		ManifestPath: "apps/" + appName + "/manifest.yaml", ReleaseMetadata: release,
		PublicationKind: appregistry.PublicationKindLocal,
		LocalSource:     &appregistry.LocalSourceState{CommitSHA: "651a5c30feb995c9364c38f63d0d5c3880bc2055"},
		BuilderVersion:  "0.0.1-test-builder",
		Artifacts: []appregistry.PublishDeclarationArtifact{{
			Platform: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: digest, Size: int64(len(artifactBytes)),
		}},
	}, artifactBytes
}

type publishHTTPResult struct {
	PublishID string `json:"publishId"`
	State     string `json:"state"`
	Uploads   []struct {
		UploadURL string `json:"uploadUrl"`
	} `json:"uploads"`
}

func postPublishBegin(t *testing.T, baseURL string, declaration *appregistry.PublishDeclaration) publishHTTPResult {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"declaration": declaration})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST begin: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST begin status = %d: %s", resp.StatusCode, responseBody)
	}
	var created publishHTTPResult
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode begin: %v", err)
	}
	return created
}

func postPublishFinalize(t *testing.T, baseURL, publishID string, declaration *appregistry.PublishDeclaration) publishHTTPResult {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"declaration": declaration})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/v1/apps/g-issues/admin/registry/publishes/"+publishID+"/finalize", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST finalize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST finalize status = %d: %s", resp.StatusCode, responseBody)
	}
	var finalized publishHTTPResult
	if err := json.NewDecoder(resp.Body).Decode(&finalized); err != nil {
		t.Fatalf("decode finalize: %v", err)
	}
	return finalized
}

func postPublishFinalizeStatus(t *testing.T, baseURL, publishID string, declaration *appregistry.PublishDeclaration) int {
	t.Helper()
	return postPublishFinalizeStatusForApp(t, baseURL, "g-issues", publishID, declaration)
}

func postPublishFinalizeStatusForApp(t *testing.T, baseURL, app, publishID string, declaration *appregistry.PublishDeclaration) int {
	t.Helper()
	url := baseURL + "/api/v1/apps/" + app + "/admin/registry/publishes/" + publishID + "/finalize"
	body, _ := json.Marshal(map[string]any{"declaration": declaration})
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST finalize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func TestAppAdminRegistryPublishRejectsMissingBuilderVersion(t *testing.T) {
	t.Parallel()

	harness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID(testCanonicalAdminUserID)
	authz := &serverTestAuthorizationProvider{
		relationships: []*proto.Relationship{
			testAuthorizationRelationship(subjectID, "admin", "app", "g-issues"),
		},
	}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("alice-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": harness.registry}
		cfg.AppRegistryPublish = harness.service
	})
	testutil.CloseOnCleanup(t, ts)

	declaration := *harness.declaration
	declaration.BuilderVersion = ""
	body, _ := json.Marshal(map[string]any{"declaration": &declaration})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer alice-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST publish: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s, want 400 for missing builderVersion", resp.StatusCode, responseBody)
	}
}

func stubCheckUploadSigning(t *testing.T, err error) func() {
	t.Helper()
	prev := server.CheckUploadSigningForTest()
	server.SetCheckUploadSigningForTest(func(*appregistry.GCSUploadSigner) error { return err })
	return func() { server.SetCheckUploadSigningForTest(prev) }
}

func stubCheckGCSRegistryPermissions(t *testing.T, err error) func() {
	t.Helper()
	prev := server.CheckGCSRegistryPermissionsForTest()
	server.SetCheckGCSRegistryPermissionsForTest(func(context.Context, string) error { return err })
	return func() { server.SetCheckGCSRegistryPermissionsForTest(prev) }
}
