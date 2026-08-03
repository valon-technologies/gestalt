package authorization

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	openfga "github.com/openfga/go-sdk"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestBootstrapAuthorizationStateOnlyWritesConfiguredRelationships(t *testing.T) {
	t.Parallel()

	var paths []string
	var writeBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/stores/test-store/authorization-models":
			_, _ = w.Write([]byte(`{"authorization_model_id":"model-id"}`))
		case "/stores/test-store/write":
			if err := json.NewDecoder(r.Body).Decode(&writeBody); err != nil {
				t.Errorf("decode write request: %v", err)
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	cfg, err := openfga.NewConfiguration(openfga.Configuration{ApiUrl: server.URL})
	if err != nil {
		t.Fatalf("NewConfiguration: %v", err)
	}
	provider := &openFGA{
		client:  openfga.NewAPIClient(cfg),
		storeID: "test-store",
		meta:    make(map[string]*proto.Relationship),
	}
	model := &proto.AuthorizationModel{Id: "logical-id", Version: "1", ResourceTypes: []*proto.AuthorizationModelResourceType{{
		Name: "repository",
		Relations: []*proto.ModelRelation{{
			Name: "viewer",
			AllowedTargets: []*proto.ModelAllowedTarget{{
				Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"},
			}},
		}},
	}}}
	relationship := &proto.Relationship{Tuple: &proto.RelationshipTuple{
		Target: &proto.RelationshipTarget{Kind: &proto.RelationshipTarget_Subject{Subject: &proto.Subject{
			Type: "subject",
			Id:   "user:alice",
		}}},
		Relation: "viewer",
		Resource: &proto.Resource{Type: "repository", Id: "repo-1"},
	}}

	if err := provider.BootstrapAuthorizationState(t.Context(), model, []*proto.Relationship{relationship}); err != nil {
		t.Fatalf("BootstrapAuthorizationState: %v", err)
	}
	if got, want := strings.Join(paths, ","), "/stores/test-store/authorization-models,/stores/test-store/write"; got != want {
		t.Fatalf("request paths = %q, want %q", got, want)
	}
	if _, ok := writeBody["deletes"]; ok {
		t.Fatalf("bootstrap write unexpectedly contains deletes: %#v", writeBody)
	}
	if _, ok := writeBody["writes"]; !ok {
		t.Fatalf("bootstrap write does not contain configured relationships: %#v", writeBody)
	}
}

func TestNewFGACodecPreservesGraphTargetsAndStableNames(t *testing.T) {
	t.Parallel()

	model := &proto.AuthorizationModel{ResourceTypes: []*proto.AuthorizationModelResourceType{
		{
			Name:        "workspace.document",
			DefaultRole: "viewer",
			Relations: []*proto.ModelRelation{
				{Name: "viewer", AllowedTargets: []*proto.ModelAllowedTarget{
					{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"}},
					{Kind: &proto.ModelAllowedTarget_SubjectSetType{SubjectSetType: &proto.SubjectSetType{ResourceType: "group", Relation: "member"}}},
				}},
			},
			Actions: []*proto.ModelAction{{Name: "read", Relations: []string{"viewer"}}},
		},
		{Name: "group", Relations: []*proto.ModelRelation{{Name: "member", AllowedTargets: []*proto.ModelAllowedTarget{{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"}}}}}},
	}}

	codec, err := newFGACodec(model)
	if err != nil {
		t.Fatalf("newFGACodec: %v", err)
	}
	if !strings.HasPrefix(codec.types["workspace.document"], "t_") || !strings.HasPrefix(codec.relations["workspace.document\x00viewer"], "r_") || !strings.HasPrefix(codec.permissions["workspace.document\x00read"], "p_") {
		t.Fatalf("unexpected encoded names: types=%#v relations=%#v permissions=%#v", codec.types, codec.relations, codec.permissions)
	}
	request, err := codec.modelRequest()
	if err != nil {
		t.Fatalf("modelRequest: %v", err)
	}
	if len(request.TypeDefinitions) != 3 {
		t.Fatalf("type definitions = %d, want 3 resource/subject-set definitions", len(request.TypeDefinitions))
	}
	if len(request.TypeDefinitions[0].GetRelations()) != 2 {
		t.Fatalf("first type relations = %#v, want viewer/read", request.TypeDefinitions[0].GetRelations())
	}
	metadata := request.TypeDefinitions[0].GetMetadata()
	viewer := metadata.GetRelations()[codec.relations["workspace.document\x00viewer"]]
	directTypes := viewer.GetDirectlyRelatedUserTypes()
	if len(directTypes) != 2 {
		t.Fatalf("viewer direct types = %#v, want configured subject and group types only", directTypes)
	}
	if got := directTypes[0].GetType(); got != codec.types["subject"] {
		t.Fatalf("subject relation type = %q, want encoded subject type %q", got, codec.types["subject"])
	}
	if got := directTypes[1].GetType(); got != codec.types["group"] {
		t.Fatalf("subject-set relation type = %q, want encoded group type %q", got, codec.types["group"])
	}
	permission := request.TypeDefinitions[0].GetRelations()[codec.permissions["workspace.document\x00read"]]
	computed, ok := permission.GetComputedUsersetOk()
	if !ok {
		t.Fatalf("read permission = %#v, want computed userset", permission)
	}
	if got := computed.GetObject(); got != "" {
		t.Fatalf("computed userset object = %q, want empty same-object reference", got)
	}
	if got := computed.GetRelation(); got != codec.relations["workspace.document\x00viewer"] {
		t.Fatalf("computed userset relation = %q, want encoded viewer relation %q", got, codec.relations["workspace.document\x00viewer"])
	}
}

func TestOpenFGADefaultRoleCompatibility(t *testing.T) {
	t.Parallel()

	var checkRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkRequests++
		if r.URL.Path != "/stores/test-store/check" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allowed":false}`))
	}))
	t.Cleanup(server.Close)

	clientConfig, err := openfga.NewConfiguration(openfga.Configuration{ApiUrl: server.URL})
	if err != nil {
		t.Fatalf("NewConfiguration: %v", err)
	}
	model := &proto.AuthorizationModel{Id: "model-id", ResourceTypes: []*proto.AuthorizationModelResourceType{{
		Name:        "document",
		DefaultRole: "viewer",
		Relations: []*proto.ModelRelation{
			{Name: "viewer", AllowedTargets: []*proto.ModelAllowedTarget{{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"}}}},
			{Name: "editor", AllowedTargets: []*proto.ModelAllowedTarget{{Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: "subject"}}}},
		},
		Actions: []*proto.ModelAction{
			{Name: "read", Relations: []string{"viewer"}},
			{Name: "edit", Relations: []string{"editor"}},
		},
	}}}
	codec, err := newFGACodec(model)
	if err != nil {
		t.Fatalf("newFGACodec: %v", err)
	}
	provider := &openFGA{
		client:   openfga.NewAPIClient(clientConfig),
		storeID:  "test-store",
		model:    model,
		modelRef: &proto.AuthorizationModelRef{Id: "model-id"},
		codec:    codec,
		meta:     make(map[string]*proto.Relationship),
	}
	request := func(action string, subject *proto.Subject) *proto.CheckAccessRequest {
		return &proto.CheckAccessRequest{
			Subject:  subject,
			Resource: &proto.Resource{Type: "document", Id: "doc-1"},
			Action:   &proto.Action{Name: action},
		}
	}
	subject := &proto.Subject{Type: "subject", Id: "user:alice"}

	allowed, err := provider.CheckAccess(t.Context(), request("read", subject))
	if err != nil {
		t.Fatalf("default-role CheckAccess: %v", err)
	}
	if !allowed.GetAllowed() {
		t.Fatal("default-role action denied, want allowed")
	}
	if checkRequests != 0 {
		t.Fatalf("default-role action made %d OpenFGA checks, want 0", checkRequests)
	}

	denied, err := provider.CheckAccess(t.Context(), request("edit", subject))
	if err != nil {
		t.Fatalf("non-default-role CheckAccess: %v", err)
	}
	if denied.GetAllowed() {
		t.Fatal("action excluding defaultRole allowed, want denied")
	}
	if checkRequests != 1 {
		t.Fatalf("non-default-role action made %d OpenFGA checks, want 1", checkRequests)
	}

	scopedSubject := &proto.Subject{Type: "subject", Id: "user:alice", Properties: func() *structpb.Struct {
		value, err := structpb.NewStruct(map[string]any{"scope": "other:operation"})
		if err != nil {
			t.Fatalf("NewStruct: %v", err)
		}
		return value
	}()}
	scopeDenied, err := provider.CheckAccess(t.Context(), request("read", scopedSubject))
	if err != nil {
		t.Fatalf("scoped default-role CheckAccess: %v", err)
	}
	if scopeDenied.GetAllowed() {
		t.Fatal("scope-denied default-role action allowed, want denied")
	}
	if checkRequests != 1 {
		t.Fatalf("scope-denied action made %d OpenFGA checks, want 1", checkRequests)
	}
}

func TestOpenFGAListActiveModelResourceTypesFiltersSourceLayerBeforePagination(t *testing.T) {
	t.Parallel()

	model := &proto.AuthorizationModel{Id: "model-id", ResourceTypes: []*proto.AuthorizationModelResourceType{
		{Name: "static-alpha", SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG},
		{Name: "runtime-alpha", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME},
		{Name: "static-beta", SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG},
		{Name: "runtime-beta", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME},
	}}
	provider := &openFGA{model: model}

	tests := []struct {
		name      string
		filter    *proto.AuthorizationModelResourceTypeFilter
		pageSize  int32
		pageToken string
		wantNames []string
		wantNext  string
	}{
		{
			name:      "static-only",
			filter:    &proto.AuthorizationModelResourceTypeFilter{SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG},
			wantNames: []string{"static-alpha", "static-beta"},
		},
		{
			name:      "runtime-only",
			filter:    &proto.AuthorizationModelResourceTypeFilter{SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME},
			wantNames: []string{"runtime-alpha", "runtime-beta"},
		},
		{
			name:      "combined-name-and-layer",
			filter:    &proto.AuthorizationModelResourceTypeFilter{Name: "runtime-beta", SourceLayer: proto.SourceLayer_SOURCE_LAYER_RUNTIME},
			wantNames: []string{"runtime-beta"},
		},
		{
			name:      "pagination-after-filtering",
			filter:    &proto.AuthorizationModelResourceTypeFilter{SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG},
			pageSize:  1,
			wantNames: []string{"static-alpha"},
			wantNext:  "1",
		},
		{
			name:      "pagination-second-page",
			filter:    &proto.AuthorizationModelResourceTypeFilter{SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG},
			pageSize:  1,
			pageToken: "1",
			wantNames: []string{"static-beta"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response, err := provider.ListActiveModelResourceTypes(t.Context(), &proto.ListActiveModelResourceTypesRequest{
				Filter:    test.filter,
				PageSize:  test.pageSize,
				PageToken: test.pageToken,
			})
			if err != nil {
				t.Fatalf("ListActiveModelResourceTypes: %v", err)
			}
			gotNames := make([]string, 0, len(response.ResourceTypes))
			for _, resourceType := range response.ResourceTypes {
				gotNames = append(gotNames, resourceType.GetName())
			}
			if !reflect.DeepEqual(gotNames, test.wantNames) {
				t.Fatalf("resource types = %#v, want %#v", gotNames, test.wantNames)
			}
			if response.NextPageToken != test.wantNext {
				t.Fatalf("next page token = %q, want %q", response.NextPageToken, test.wantNext)
			}
		})
	}
}

func TestFGAWriteBatchesRespectCombinedLimit(t *testing.T) {
	t.Parallel()

	writes := make([]openfga.TupleKey, 75)
	deletes := make([]openfga.TupleKeyWithoutCondition, 80)
	batches := fgaWriteBatches(writes, deletes, 100)
	if len(batches) != 2 {
		t.Fatalf("batches = %d, want 2", len(batches))
	}
	for i, batch := range batches {
		if got := len(batch.writes) + len(batch.deletes); got > 100 {
			t.Fatalf("batch %d contains %d operations, want at most 100", i, got)
		}
	}
	if got := len(batches[0].writes); got != 75 {
		t.Fatalf("first batch writes = %d, want 75", got)
	}
	if got := len(batches[0].deletes); got != 25 {
		t.Fatalf("first batch deletes = %d, want 25", got)
	}
	if got := len(batches[1].deletes); got != 55 {
		t.Fatalf("second batch deletes = %d, want 55", got)
	}
}

func TestSplitFGAObjectRejectsMalformedValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "missing-separator", ":id", "type:"} {
		if _, _, ok := splitFGAObject(value); ok {
			t.Fatalf("splitFGAObject(%q) accepted malformed value", value)
		}
	}
	logicalType, id, ok := splitFGAObject("type:id:with-colon")
	if !ok || logicalType != "type" || id != "id:with-colon" {
		t.Fatalf("splitFGAObject returned (%q, %q, %v)", logicalType, id, ok)
	}
}
