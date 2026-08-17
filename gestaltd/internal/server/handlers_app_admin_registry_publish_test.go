package server_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
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

	publishHarness := newRegistryPublishHarness(t)
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
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": publishHarness.registry}
		cfg.AppRegistryPublish = publishHarness.service
	})

	createBody, _ := json.Marshal(map[string]any{"declaration": publishHarness.declaration})
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer alice-token")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("POST publish create: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	var created struct {
		Uploads []struct {
			Headers map[string]string `json:"headers"`
		} `json:"uploads"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if len(created.Uploads) != 1 || created.Uploads[0].Headers == nil {
		t.Fatalf("create uploads = %#v", created.Uploads)
	}
	if created.Uploads[0].Headers[appregistry.UploadHeaderXGoogMetaSHA256] == "" {
		t.Fatalf("headers = %#v", created.Uploads[0].Headers)
	}
}

func TestAppAdminRegistryPublishConcurrentFinalize(t *testing.T) {
	t.Parallel()

	publishHarness := newRegistryPublishHarness(t)
	limits := publishHarness.service.Limits
	limits.FinalizeClaimLeaseTTL = 30 * time.Minute
	publishHarness.service.Limits = limits
	claimed := make(chan struct{})
	proceed := make(chan struct{})
	var claimOnce sync.Once
	publishHarness.service.FinalizeAfterClaimHook = func() {
		claimOnce.Do(func() { close(claimed) })
		<-proceed
	}
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
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": publishHarness.registry}
		cfg.AppRegistryReader = publishHarness.reader
		cfg.AppRegistryPublish = publishHarness.service
	})

	createBody, _ := json.Marshal(map[string]any{"declaration": publishHarness.declaration})
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer alice-token")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("POST publish create: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	var created struct {
		PublishID string `json:"publishId"`
		Uploads   []struct {
			UploadURL string `json:"uploadUrl"`
		} `json:"uploads"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if err := appregistry.ApplyMemoryUpload(publishHarness.mem, created.Uploads[0].UploadURL, publishHarness.artifactBytes, publishHarness.declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	const competitors = 5
	ownerDone := make(chan int, 1)
	go func() {
		finalReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes/"+created.PublishID+"/finalize", nil)
		finalReq.Header.Set("Authorization", "Bearer alice-token")
		finalResp, err := http.DefaultClient.Do(finalReq)
		if err != nil {
			t.Errorf("POST owner finalize: %v", err)
			ownerDone <- 0
			return
		}
		defer func() { _ = finalResp.Body.Close() }()
		ownerDone <- finalResp.StatusCode
	}()
	<-claimed

	var wg sync.WaitGroup
	statuses := make(chan int, competitors)
	for i := 0; i < competitors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			finalReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes/"+created.PublishID+"/finalize", nil)
			finalReq.Header.Set("Authorization", "Bearer alice-token")
			finalResp, err := http.DefaultClient.Do(finalReq)
			if err != nil {
				t.Errorf("POST finalize: %v", err)
				statuses <- 0
				return
			}
			defer func() { _ = finalResp.Body.Close() }()
			statuses <- finalResp.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)

	var conflictCount int
	for status := range statuses {
		if status == http.StatusConflict {
			conflictCount++
			continue
		}
		t.Fatalf("unexpected competitor finalize status = %d", status)
	}
	if conflictCount != competitors {
		t.Fatalf("conflict=%d, want %d", conflictCount, competitors)
	}

	close(proceed)
	ownerStatus := <-ownerDone
	if ownerStatus != http.StatusOK {
		t.Fatalf("owner finalize status = %d, want 200", ownerStatus)
	}

	retryReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes/"+created.PublishID+"/finalize", nil)
	retryReq.Header.Set("Authorization", "Bearer alice-token")
	retryResp, err := http.DefaultClient.Do(retryReq)
	if err != nil {
		t.Fatalf("POST retry finalize: %v", err)
	}
	defer func() { _ = retryResp.Body.Close() }()
	if retryResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(retryResp.Body)
		t.Fatalf("retry finalize status = %d: %s", retryResp.StatusCode, body)
	}
}

func TestAppAdminRegistryPublishAuthAndFlow(t *testing.T) {
	t.Parallel()

	publishHarness := newRegistryPublishHarness(t)
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
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": publishHarness.registry}
		cfg.AppRegistryReader = publishHarness.reader
		cfg.AppRegistryPublish = publishHarness.service
	})

	createBody, _ := json.Marshal(map[string]any{"declaration": publishHarness.declaration})
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer alice-token")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("POST publish create: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("POST create status = %d: %s", createResp.StatusCode, body)
	}
	var created struct {
		PublishID string `json:"publishId"`
		Uploads   []struct {
			Platform  string `json:"platform"`
			UploadURL string `json:"uploadUrl"`
		} `json:"uploads"`
		Publisher string `json:"publisher"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.PublishID == "" || len(created.Uploads) != 1 || created.Publisher == "" {
		t.Fatalf("create response = %#v", created)
	}
	if err := appregistry.ApplyMemoryUpload(publishHarness.mem, created.Uploads[0].UploadURL, publishHarness.artifactBytes, publishHarness.declaration.Artifacts[0].SHA256); err != nil {
		t.Fatalf("upload: %v", err)
	}

	getReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes/"+created.PublishID, nil)
	getReq.Header.Set("Authorization", "Bearer alice-token")
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET publish: %v", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("GET status = %d: %s", getResp.StatusCode, body)
	}

	finalReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes/"+created.PublishID+"/finalize", nil)
	finalReq.Header.Set("Authorization", "Bearer alice-token")
	finalResp, err := http.DefaultClient.Do(finalReq)
	if err != nil {
		t.Fatalf("POST finalize: %v", err)
	}
	defer func() { _ = finalResp.Body.Close() }()
	if finalResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalResp.Body)
		t.Fatalf("POST finalize status = %d: %s", finalResp.StatusCode, body)
	}

	registryReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/g-issues/admin/registry", nil)
	registryReq.Header.Set("Authorization", "Bearer alice-token")
	registryResp, err := http.DefaultClient.Do(registryReq)
	if err != nil {
		t.Fatalf("GET registry: %v", err)
	}
	defer func() { _ = registryResp.Body.Close() }()
	var registryState struct {
		PublishSessions []struct {
			PublishID string `json:"publishId"`
		} `json:"publishSessions"`
	}
	if err := json.NewDecoder(registryResp.Body).Decode(&registryState); err != nil {
		t.Fatalf("decode registry: %v", err)
	}
	if len(registryState.PublishSessions) != 0 {
		t.Fatalf("active publish sessions = %#v", registryState.PublishSessions)
	}
}

func TestAppAdminRegistryPublishRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	publishHarness := newRegistryPublishHarness(t)
	subjectID := principal.UserSubjectID("bob")
	authz := &serverTestAuthorizationProvider{relationships: nil}
	ts := newTestServer(t, func(cfg *server.Config) {
		cfg.Auth = authStubWithSessionTokenIntrospect("bob-token", subjectID, "")
		cfg.Authorization = authz
		cfg.AppDefs = map[string]*config.ProviderEntry{
			"g-issues": {Source: config.ProviderSource{Registry: "toolshed"}},
		}
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": publishHarness.registry}
		cfg.AppRegistryPublish = publishHarness.service
	})
	body, _ := json.Marshal(map[string]any{"declaration": publishHarness.declaration})
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

func TestAppAdminRegistryPublishRejectsCrossApp(t *testing.T) {
	t.Parallel()

	publishHarness := newRegistryPublishHarness(t)
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
		cfg.AppRegistries = map[string]config.AppRegistryConfig{"toolshed": publishHarness.registry}
		cfg.AppRegistryPublish = publishHarness.service
	})
	body, _ := json.Marshal(map[string]any{"declaration": publishHarness.declaration})
	createReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/apps/g-issues/admin/registry/publishes", bytes.NewReader(body))
	createReq.Header.Set("Authorization", "Bearer alice-token")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("POST publish create: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	var created struct {
		PublishID string `json:"publishId"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/apps/other-app/admin/registry/publishes/"+created.PublishID, nil)
	req.Header.Set("Authorization", "Bearer alice-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET cross-app publish: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, body)
	}
}

type registryPublishHarness struct {
	registry      config.AppRegistryConfig
	reader        *appregistry.RegistryReader
	service       *appregistry.PublishSessionService
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
	services := testutil.NewStubServices(t)
	store, mem := appregistry.NewMemoryPublishStores()
	signer := appregistry.NewMemoryRegistryUploadSigner(mem, "memory-upload://")
	limits := appregistry.DefaultPublishSessionLimits()
	limits.RequiredPlatforms = []string{"linux/amd64"}
	service := &appregistry.PublishSessionService{
		Sessions: services.AppRegistryPublishSessions,
		Store:    store,
		Signer:   signer,
		Writer:   &appregistry.Writer{Store: store},
		Index:    appregistry.StoreIndexChecker{Store: store, StorageRoot: "gs://gitlab-peach-street-gestalt-app-registry"},
		Limits:   limits,
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
		Schema:        providerrelease.SchemaName,
		SchemaVersion: providerrelease.SchemaVersion,
		Package:       "github.com/valon-technologies/valon-tools/apps/" + appName,
		Kind:          providermanifestv1.KindApp,
		Version:       version,
		Runtime:       providerrelease.RuntimeExecutable,
		Artifacts: providerrelease.Artifacts{
			"linux/amd64": {Path: "linux-amd64.tar.gz", SHA256: digest},
		},
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
		Artifacts: []appregistry.PublishDeclarationArtifact{{
			Platform: "linux/amd64", Filename: "linux-amd64.tar.gz", SHA256: digest, Size: int64(len(artifactBytes)),
		}},
	}, artifactBytes
}

func init() {
	_ = core.AppRegistryPublishSessionUploading
	_ = time.Second
}
