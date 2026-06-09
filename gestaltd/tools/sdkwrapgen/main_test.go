package main

import (
	"strings"
	"testing"

	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestValidateAuthorizationDescriptorContract(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version: 1,
		Services: []serviceConfig{{
			Proto:   "gestalt.provider.v1.AuthorizationProvider",
			SDKName: "Authorization",
			Package: map[string]string{
				"typescript": "Authorization",
				"python":     "authorization",
				"go":         "authorization",
				"rust":       "authorization",
			},
			WellKnownTypes: map[string]wellKnownTypeRule{
				"google.protobuf.Struct":    {Semantic: "json_object"},
				"google.protobuf.Timestamp": {Semantic: "timestamp_with_presence"},
			},
			EnumPolicy:  "preserve_unknown_numeric",
			OneofPolicy: "explicit_variants",
		}},
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}

	image := fileDescriptorSet(protov1.File_v1_authorization_proto)
	services, err := validateImage(cfg, image)
	if err != nil {
		t.Fatalf("validateImage: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("services len = %d, want 1", len(services))
	}
}

func fileDescriptorSet(file protoreflect.FileDescriptor) *descriptorpb.FileDescriptorSet {
	files := []*descriptorpb.FileDescriptorProto{}
	seen := map[string]bool{}
	var visit func(protoreflect.FileDescriptor)
	visit = func(current protoreflect.FileDescriptor) {
		if seen[current.Path()] {
			return
		}
		seen[current.Path()] = true
		imports := current.Imports()
		for i := 0; i < imports.Len(); i++ {
			visit(imports.Get(i).FileDescriptor)
		}
		files = append(files, protodesc.ToFileDescriptorProto(current))
	}
	visit(file)
	return &descriptorpb.FileDescriptorSet{File: files}
}

func TestValidateAuthorizationDescriptorContractRequiresTimestampRule(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version: 1,
		Services: []serviceConfig{{
			Proto:   "gestalt.provider.v1.AuthorizationProvider",
			SDKName: "Authorization",
			Package: map[string]string{
				"typescript": "Authorization",
				"python":     "authorization",
				"go":         "authorization",
				"rust":       "authorization",
			},
			WellKnownTypes: map[string]wellKnownTypeRule{
				"google.protobuf.Struct":    {Semantic: "json_object"},
				"google.protobuf.Timestamp": {Semantic: "json_object"},
			},
			EnumPolicy:  "preserve_unknown_numeric",
			OneofPolicy: "explicit_variants",
		}},
	}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("validateConfig succeeded, want timestamp semantic error")
	}
}

func TestValidateImageReportsDescriptorParseErrors(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version: 1,
		Services: []serviceConfig{{
			Proto:   "gestalt.provider.v1.AuthorizationProvider",
			SDKName: "Authorization",
			Package: map[string]string{
				"typescript": "Authorization",
				"python":     "authorization",
				"go":         "authorization",
				"rust":       "authorization",
			},
			WellKnownTypes: map[string]wellKnownTypeRule{
				"google.protobuf.Struct":    {Semantic: "json_object"},
				"google.protobuf.Timestamp": {Semantic: "timestamp_with_presence"},
			},
			EnumPolicy:  "preserve_unknown_numeric",
			OneofPolicy: "explicit_variants",
		}},
	}
	missing := "missing.proto"
	broken := "broken.proto"
	_, err := validateImage(cfg, &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:       &broken,
			Dependency: []string{missing},
		}},
	})
	if err == nil {
		t.Fatal("validateImage succeeded, want descriptor parse error")
	}
	if !strings.Contains(err.Error(), "parse descriptor image") {
		t.Fatalf("error = %q, want parse descriptor image", err)
	}
}
