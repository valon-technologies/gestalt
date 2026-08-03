package authorization

import (
	"strings"
	"testing"

	openfga "github.com/openfga/go-sdk"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

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
	if got := directTypes[1].GetType(); got != codec.types["group"] {
		t.Fatalf("subject-set relation type = %q, want encoded group type %q", got, codec.types["group"])
	}
	if got := directTypes[2].GetType(); got != codec.types["subject"] {
		t.Fatalf("default wildcard type = %q, want encoded subject type %q", got, codec.types["subject"])
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
