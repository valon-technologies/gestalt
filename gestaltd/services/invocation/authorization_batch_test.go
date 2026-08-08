package invocation

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	coretesting "github.com/valon-technologies/gestalt/server/core/testing"
	"github.com/valon-technologies/gestalt/server/internal/testutil"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

// batchAuthorizationProvider models a provider-owned evaluator that expands
// subject sets, so a subject holding a role only through a group is allowed
// exactly as the real relationship-graph provider allows it. It counts calls so
// tests can prove a listing costs one batched round trip, not one per entry.
type batchAuthorizationProvider struct {
	core.AuthorizationProvider

	// allow maps "<subject>|<action>" to the relations that authorize it.
	allow map[string][]string

	checkAccessCalls     int
	checkAccessManyCalls int
	checkAccessErr       error
	checkAccessManyErr   error
}

func (p *batchAuthorizationProvider) CheckAccess(_ context.Context, req *proto.CheckAccessRequest) (*proto.CheckAccessResponse, error) {
	p.checkAccessCalls++
	if p.checkAccessErr != nil {
		return nil, p.checkAccessErr
	}
	relations := p.allow[req.GetSubject().GetId()+"|"+req.GetAction().GetName()]
	return &proto.CheckAccessResponse{Allowed: len(relations) > 0, MatchedRelations: relations}, nil
}

func (p *batchAuthorizationProvider) CheckAccessMany(ctx context.Context, req *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	p.checkAccessManyCalls++
	if p.checkAccessManyErr != nil {
		return nil, p.checkAccessManyErr
	}
	decisions := make([]*proto.CheckAccessResponse, 0, len(req.GetRequests()))
	for _, entry := range req.GetRequests() {
		// Reuse the single-decision path so the stub cannot drift either.
		p.checkAccessCalls--
		decision, err := p.CheckAccess(ctx, entry)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return &proto.CheckAccessManyResponse{Decisions: decisions}, nil
}

func (p *batchAuthorizationProvider) Ping(context.Context) error { return nil }

func (p *batchAuthorizationProvider) Close() error { return nil }

func batchTestCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		Name: "slack",
		Operations: []catalog.CatalogOperation{
			{ID: "chat.postMessage", Method: "POST", Transport: catalog.TransportApp},
			{ID: "chat.delete", Method: "POST", Transport: catalog.TransportApp},
			{ID: "admin.purge", Method: "POST", Transport: catalog.TransportApp, AllowedRoles: []string{"admin"}},
		},
	}
}

func batchTestBroker(t *testing.T, authz core.AuthorizationProvider) *Broker {
	t.Helper()
	provider := &coretesting.StubIntegration{
		N:          "slack",
		ConnMode:   core.ConnectionModeNone,
		CatalogVal: batchTestCatalog(),
	}
	return NewBroker(
		testutil.NewProviderRegistry(t, provider),
		nil,
		nil,
		WithAuthorizationProvider(authz),
		WithProviderKinds(map[string]ProviderKind{"slack": ProviderKindApp}),
	)
}

func batchTestPrincipal() *principal.Principal {
	return &principal.Principal{SubjectID: "user:u-123", UserID: "u-123", Kind: principal.KindUser}
}

// TestFilterCatalogForPrincipalIssuesOneBatchedCall is the call-count guard:
// filtering a three-operation catalog must cost one CheckAccessMany and zero
// per-operation CheckAccess calls.
func TestFilterCatalogForPrincipalIssuesOneBatchedCall(t *testing.T) {
	t.Parallel()

	authz := &batchAuthorizationProvider{allow: map[string][]string{
		"user:u-123|chat.postMessage": {"user"},
		"user:u-123|chat.delete":      {"user"},
	}}
	broker := batchTestBroker(t, authz)

	filtered, err := FilterCatalogForPrincipal(
		context.Background(), batchTestCatalog(), "slack", batchTestPrincipal(), broker)
	if err != nil {
		t.Fatalf("FilterCatalogForPrincipal: %v", err)
	}
	if len(filtered.Operations) != 2 {
		t.Fatalf("filtered operations = %d, want 2: %#v", len(filtered.Operations), filtered.Operations)
	}
	if authz.checkAccessManyCalls != 1 {
		t.Fatalf("CheckAccessMany calls = %d, want 1", authz.checkAccessManyCalls)
	}
	if authz.checkAccessCalls != 0 {
		t.Fatalf("CheckAccess calls = %d, want 0 (listing must batch)", authz.checkAccessCalls)
	}
}

// TestFilterCatalogForPrincipalHidesOnlyDeniedOperations proves an ungranted
// subject sees nothing while a granted subject keeps its operations, and that
// an operation's AllowedRoles are applied at listing exactly as at invoke.
func TestFilterCatalogForPrincipalHidesOnlyDeniedOperations(t *testing.T) {
	t.Parallel()

	ungranted := &batchAuthorizationProvider{allow: map[string][]string{}}
	filtered, err := FilterCatalogForPrincipal(
		context.Background(), batchTestCatalog(), "slack", batchTestPrincipal(), batchTestBroker(t, ungranted))
	if err != nil {
		t.Fatalf("FilterCatalogForPrincipal: %v", err)
	}
	if len(filtered.Operations) != 0 {
		t.Fatalf("ungranted subject sees %#v, want none", filtered.Operations)
	}

	// The subject is allowed the role-restricted operation but the evaluator
	// names a relation the operation does not accept, which is a deny at invoke
	// time; listing must agree.
	wrongRole := &batchAuthorizationProvider{allow: map[string][]string{
		"user:u-123|admin.purge": {"viewer"},
	}}
	filtered, err = FilterCatalogForPrincipal(
		context.Background(), batchTestCatalog(), "slack", batchTestPrincipal(), batchTestBroker(t, wrongRole))
	if err != nil {
		t.Fatalf("FilterCatalogForPrincipal: %v", err)
	}
	if len(filtered.Operations) != 0 {
		t.Fatalf("role-mismatched operation was listed: %#v", filtered.Operations)
	}
}

// TestCheckOperationAccessManyMatchesSingleDecision is the semantic-identity
// guard: for every operation, the batched answer equals the answer the
// single-decision path gives for the same question.
func TestCheckOperationAccessManyMatchesSingleDecision(t *testing.T) {
	t.Parallel()

	allow := map[string][]string{
		"user:u-123|chat.postMessage": {"user"},
		"user:u-123|admin.purge":      {"admin"},
	}
	p := batchTestPrincipal()
	queries := []OperationAccessQuery{
		{Provider: "slack", Operation: "chat.postMessage"},
		{Provider: "slack", Operation: "chat.delete"},
		{Provider: "slack", Operation: "admin.purge", AllowedRoles: []string{"admin"}},
		{Provider: "slack", Operation: "admin.purge", AllowedRoles: []string{"viewer"}},
	}

	batchAuthz := &batchAuthorizationProvider{allow: allow}
	batched, err := batchTestBroker(t, batchAuthz).CheckOperationAccessMany(context.Background(), p, queries)
	if err != nil {
		t.Fatalf("CheckOperationAccessMany: %v", err)
	}
	singleAuthz := &batchAuthorizationProvider{allow: allow}
	singleBroker := batchTestBroker(t, singleAuthz)
	for i, query := range queries {
		single := singleBroker.CheckOperationAccess(context.Background(), p, query.Provider, query.Operation)
		// CheckOperationAccess does not apply AllowedRoles, so compare only the
		// questions where the query places no role restriction.
		if len(query.AllowedRoles) > 0 {
			continue
		}
		if (batched[i] == nil) != (single == nil) {
			t.Fatalf("query %d: batched = %v, single = %v", i, batched[i], single)
		}
	}
	if batched[2] != nil {
		t.Fatalf("admin role query denied: %v", batched[2])
	}
	if batched[3] == nil {
		t.Fatal("viewer role query allowed an admin-only operation")
	}
}

// TestCheckOperationAccessManyFallsBackWhenBatchFails proves the safety
// property that matters most: a provider that cannot serve the batch must not
// turn every entry into a denial. Listing degrades to per-item calls instead.
func TestCheckOperationAccessManyFallsBackWhenBatchFails(t *testing.T) {
	t.Parallel()

	authz := &batchAuthorizationProvider{
		allow:              map[string][]string{"user:u-123|chat.postMessage": {"user"}},
		checkAccessManyErr: errors.New("batch rpc unimplemented"),
	}
	broker := batchTestBroker(t, authz)

	results, err := broker.CheckOperationAccessMany(context.Background(), batchTestPrincipal(), []OperationAccessQuery{
		{Provider: "slack", Operation: "chat.postMessage"},
		{Provider: "slack", Operation: "chat.delete"},
	})
	if err != nil {
		t.Fatalf("CheckOperationAccessMany: %v", err)
	}
	if results[0] != nil {
		t.Fatalf("granted operation denied after batch failure: %v", results[0])
	}
	if results[1] == nil {
		t.Fatal("ungranted operation allowed after batch failure")
	}
	if authz.checkAccessCalls != 2 {
		t.Fatalf("fallback CheckAccess calls = %d, want 2", authz.checkAccessCalls)
	}
}

// TestFilterCatalogForPrincipalReturnsErrorInsteadOfEmptyCatalog is the
// access-loss guard: an unreachable evaluator must surface an error, never a
// silently empty operation list that reads as "you have no apps".
func TestFilterCatalogForPrincipalReturnsErrorInsteadOfEmptyCatalog(t *testing.T) {
	t.Parallel()

	authz := &batchAuthorizationProvider{
		allow:              map[string][]string{},
		checkAccessManyErr: errors.New("evaluator unavailable"),
		checkAccessErr:     errors.New("evaluator unavailable"),
	}
	filtered, err := FilterCatalogForPrincipal(
		context.Background(), batchTestCatalog(), "slack", batchTestPrincipal(), batchTestBroker(t, authz))
	if err == nil {
		t.Fatalf("evaluator failure produced a catalog instead of an error: %#v", filtered)
	}
	if filtered != nil {
		t.Fatalf("filtered catalog = %#v, want nil on error", filtered)
	}
}

// TestFilterCatalogForPrincipalWithoutCheckerLeavesCatalogIntact keeps the
// no-authorization deployment unchanged.
func TestFilterCatalogForPrincipalWithoutCheckerLeavesCatalogIntact(t *testing.T) {
	t.Parallel()

	cat := batchTestCatalog()
	filtered, err := FilterCatalogForPrincipal(context.Background(), cat, "slack", batchTestPrincipal(), nil)
	if err != nil {
		t.Fatalf("FilterCatalogForPrincipal: %v", err)
	}
	if len(filtered.Operations) != len(cat.Operations) {
		t.Fatalf("filtered operations = %d, want %d", len(filtered.Operations), len(cat.Operations))
	}
}

// TestCheckResourceAccessManyMatchesCheckResourceAccess pins the shared
// projection: batched and single answers agree for the same question.
func TestCheckResourceAccessManyMatchesCheckResourceAccess(t *testing.T) {
	t.Parallel()

	authz := &batchAuthorizationProvider{allow: map[string][]string{
		"user:u-1|sampleApp": {"viewer"},
	}}
	reqs := []ResourceAccessRequest{
		{SubjectID: "user:u-1", Action: "sampleApp", Resource: &proto.Resource{Type: "app", Id: "sampleApp"}},
		{SubjectID: "user:u-1", Action: "sampleApp", Resource: &proto.Resource{Type: "app", Id: "sampleApp"}, AllowedRoles: []string{"admin"}},
		{SubjectID: "user:u-2", Action: "sampleApp", Resource: &proto.Resource{Type: "app", Id: "sampleApp"}},
		{SubjectID: "", Action: "sampleApp", Resource: &proto.Resource{Type: "app", Id: "sampleApp"}},
	}

	batched, err := CheckResourceAccessMany(context.Background(), authz, reqs)
	if err != nil {
		t.Fatalf("CheckResourceAccessMany: %v", err)
	}
	for i, req := range reqs {
		single, singleErr := CheckResourceAccess(context.Background(), authz, req)
		if singleErr != nil {
			t.Fatalf("req %d: CheckResourceAccess: %v", i, singleErr)
		}
		if batched[i] != single {
			t.Fatalf("req %d: batched = %+v, single = %+v", i, batched[i], single)
		}
	}
}

// TestCheckResourceAccessManyFailsClosedOnMalformedResponse keeps a truncated
// batch from being read as a set of denials.
func TestCheckResourceAccessManyFailsClosedOnMalformedResponse(t *testing.T) {
	t.Parallel()

	authz := &truncatingAuthorizationProvider{}
	_, err := CheckResourceAccessMany(context.Background(), authz, []ResourceAccessRequest{
		{SubjectID: "user:u-1", Action: "a", Resource: &proto.Resource{Type: "app", Id: "a"}},
		{SubjectID: "user:u-1", Action: "b", Resource: &proto.Resource{Type: "app", Id: "b"}},
	})
	if !errors.Is(err, ErrMalformedAuthorizationDecision) {
		t.Fatalf("error = %v, want ErrMalformedAuthorizationDecision", err)
	}
}

type truncatingAuthorizationProvider struct {
	core.AuthorizationProvider
}

func (p *truncatingAuthorizationProvider) CheckAccessMany(context.Context, *proto.CheckAccessManyRequest) (*proto.CheckAccessManyResponse, error) {
	return &proto.CheckAccessManyResponse{Decisions: []*proto.CheckAccessResponse{{Allowed: true}}}, nil
}
