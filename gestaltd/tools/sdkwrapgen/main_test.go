package main

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	goproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestValidateProviderDescriptorContract(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{authorizationTestServiceConfig()},
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

func TestReadConfigRejectsStaleOutputPaths(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sdkgen.yaml")
	if err := os.WriteFile(path, []byte(`version: 1
services:
  - proto: gestalt.provider.v1.AuthorizationProvider
    sdk_name: Authorization
    package:
      typescript: Authorization
      python: authorization
      go: authorization
      rust: authorization
    outputs:
      typescript:
        - path: sdk/typescript/src/authorization.ts
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := readConfig(path)
	if err == nil {
		t.Fatal("readConfig succeeded, want stale outputs field error")
	}
	if !strings.Contains(err.Error(), "field outputs not found") {
		t.Fatalf("error = %q, want unknown outputs field", err)
	}
}

func TestReadCheckedInConfig(t *testing.T) {
	t.Parallel()

	cfg, err := readConfig(filepath.Join("..", "..", "..", "sdk", "sdkgen.yaml"))
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("services len = %d, want 1", len(cfg.Services))
	}
}

func TestValidateCLIInputsRequiresImageForOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		outRoot string
		printIR bool
	}{
		{name: "out root", outRoot: t.TempDir()},
		{name: "print IR", printIR: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateCLIInputs("", tc.outRoot, tc.printIR)
			if err == nil {
				t.Fatal("validateCLIInputs succeeded, want missing image error")
			}
			if !strings.Contains(err.Error(), "-image is required") {
				t.Fatalf("error = %q, want missing image error", err)
			}
		})
	}
	if err := validateCLIInputs("descriptor.binpb", t.TempDir(), true); err != nil {
		t.Fatalf("validateCLIInputs with image: %v", err)
	}
}

func TestValidateImageReportsDescriptorParseErrors(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{authorizationTestServiceConfig()},
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

func TestReadImageAcceptsTextDescriptor(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "image.textproto")
	if err := os.WriteFile(path, []byte(`file: {
  name: "v1/authorization.proto"
  package: "gestalt.provider.v1"
  syntax: "proto3"
  message_type: {
    name: "CheckAccessRequest"
  }
}
`), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	image, err := readImage(path)
	if err != nil {
		t.Fatalf("readImage: %v", err)
	}
	if len(image.GetFile()) != 1 || image.GetFile()[0].GetName() != "v1/authorization.proto" {
		t.Fatalf("image = %#v, want text descriptor file", image)
	}
}

func TestProviderSDKIRReflectsDescriptorFieldNumberChanges(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{authorizationTestServiceConfig()},
	}
	image := cloneFileDescriptorSet(fileDescriptorSet(protov1.File_v1_authorization_proto))
	field := authorizationField(t, image, "Subject", "type")
	changedNumber := int32(99)
	field.Number = &changedNumber

	services, err := validateImage(cfg, image)
	if err != nil {
		t.Fatalf("validateImage: %v", err)
	}
	ir, err := buildProviderSDKIR(cfg.Services[0], services[0])
	if err != nil {
		t.Fatalf("buildProviderSDKIR: %v", err)
	}
	subject := ir.MessagesByName["Subject"]
	if len(subject.Fields) == 0 || subject.Fields[0].Number != changedNumber {
		t.Fatalf("Subject.type number = %d, want %d", subject.Fields[0].Number, changedNumber)
	}
}

func TestProviderSDKIRReflectsDescriptorFieldTypeChanges(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{authorizationTestServiceConfig()},
	}
	image := cloneFileDescriptorSet(fileDescriptorSet(protov1.File_v1_authorization_proto))
	field := authorizationField(t, image, "CheckAccessRequest", "subject")
	changedType := ".gestalt.provider.v1.Action"
	field.TypeName = &changedType

	services, err := validateImage(cfg, image)
	if err != nil {
		t.Fatalf("validateImage: %v", err)
	}
	ir, err := buildProviderSDKIR(cfg.Services[0], services[0])
	if err != nil {
		t.Fatalf("buildProviderSDKIR: %v", err)
	}
	request := ir.MessagesByName["CheckAccessRequest"]
	assertFieldPresence(t, request, "subject", irPresenceMessage, irKindMessage)
	if request.Fields[0].MessageName != "Action" {
		t.Fatalf("CheckAccessRequest.subject message = %q, want Action", request.Fields[0].MessageName)
	}
}

func TestRenderGeneratedFilesIsDeterministic(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{authorizationTestServiceConfig()},
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("validateConfig: %v", err)
	}
	services, err := validateImage(cfg, fileDescriptorSet(protov1.File_v1_authorization_proto))
	if err != nil {
		t.Fatalf("validateImage: %v", err)
	}

	firstRoot := t.TempDir()
	first, err := writeGeneratedFiles(cfg, services, firstRoot)
	if err != nil {
		t.Fatalf("writeGeneratedFiles first: %v", err)
	}
	secondRoot := t.TempDir()
	second, err := writeGeneratedFiles(cfg, services, secondRoot)
	if err != nil {
		t.Fatalf("writeGeneratedFiles second: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("generated files len = %d, want %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Fatalf("file[%d] path = %q, want %q", i, first[i].Path, second[i].Path)
		}
		firstData, err := os.ReadFile(filepath.Join(firstRoot, filepath.FromSlash(first[i].Path)))
		if err != nil {
			t.Fatalf("read first %s: %v", first[i].Path, err)
		}
		secondData, err := os.ReadFile(filepath.Join(secondRoot, filepath.FromSlash(second[i].Path)))
		if err != nil {
			t.Fatalf("read second %s: %v", second[i].Path, err)
		}
		if !bytes.Equal(firstData, secondData) {
			t.Fatalf("file[%d] %s was not deterministic", i, first[i].Path)
		}
		if !bytes.Contains(firstData, []byte("Code generated by gestaltd/tools/sdkwrapgen. DO NOT EDIT.")) {
			t.Fatalf("file[%d] %s missing generated header", i, first[i].Path)
		}
	}
}

func TestRenderGeneratedFilesAcceptsDescriptorDerivedSecondProvider(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{exampleTestServiceConfig()},
	}
	image := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{
		Name:    goproto.String("v1/example.proto"),
		Package: goproto.String("gestalt.provider.v1"),
		Syntax:  goproto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: goproto.String("ExampleRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   goproto.String("name"),
					Number: goproto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
			{
				Name: goproto.String("ExampleResponse"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   goproto.String("ok"),
					Number: goproto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
				}},
			},
			{
				Name: goproto.String("UnusedUnsupportedMessage"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   goproto.String("count"),
					Number: goproto.Int32(1),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
				}},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: goproto.String("ExampleProvider"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       goproto.String("Check"),
				InputType:  goproto.String(".gestalt.provider.v1.ExampleRequest"),
				OutputType: goproto.String(".gestalt.provider.v1.ExampleResponse"),
			}},
		}},
	}}}
	services, err := validateImage(cfg, image)
	if err != nil {
		t.Fatalf("validateImage: %v", err)
	}
	files, err := renderGeneratedFiles(cfg, services)
	if err != nil {
		t.Fatalf("renderGeneratedFiles: %v", err)
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	want := []string{
		"sdk/go/example/client.go",
		"sdk/go/example/conversions.go",
		"sdk/go/example/doc.go",
		"sdk/go/example/protocol.go",
		"sdk/go/example/types.go",
		"sdk/python/gestalt/example.py",
		"sdk/rust/src/example.rs",
		"sdk/typescript/src/example.ts",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("generated paths:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
	for _, file := range files {
		if bytes.Contains(file.Data, []byte("UnusedUnsupportedMessage")) {
			t.Fatalf("%s emitted unreachable message", file.Path)
		}
	}
}

func TestProviderSDKIRSemantics(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{authorizationTestServiceConfig()},
	}
	services, err := validateImage(cfg, fileDescriptorSet(protov1.File_v1_authorization_proto))
	if err != nil {
		t.Fatalf("validateImage: %v", err)
	}
	ir, err := buildProviderSDKIR(cfg.Services[0], services[0])
	if err != nil {
		t.Fatalf("buildProviderSDKIR: %v", err)
	}

	subject := ir.MessagesByName["Subject"]
	assertFieldPresence(t, subject, "type", irPresenceNone, irKindString)
	assertFieldPresence(t, subject, "properties", irPresenceMessage, irKindJSON)
	assertFieldPresence(t, ir.MessagesByName["CheckAccessManyRequest"], "requests", irPresenceRepeated, irKindMessage)

	target := ir.MessagesByName["RelationshipTarget"]
	if target.Oneof == nil || target.Oneof.ProtoName != "kind" {
		t.Fatalf("RelationshipTarget oneof = %#v, want kind", target.Oneof)
	}
	if len(target.Oneof.Variants) != 3 {
		t.Fatalf("RelationshipTarget variants len = %d, want 3", len(target.Oneof.Variants))
	}
	for _, field := range target.Oneof.Variants {
		if field.Presence != irPresenceOneof {
			t.Fatalf("RelationshipTarget.%s presence = %s, want %s", field.ProtoName, field.Presence, irPresenceOneof)
		}
	}

	sourceLayer := ir.EnumsByName["SourceLayer"]
	if sourceLayer.ProtoName != "SourceLayer" || len(sourceLayer.Values) != 3 {
		t.Fatalf("SourceLayer enum = %#v", sourceLayer)
	}
}

func TestProviderSDKIRCapturesDescriptorComments(t *testing.T) {
	t.Parallel()

	file := &descriptorpb.FileDescriptorProto{
		Name:    goproto.String("commented.proto"),
		Package: goproto.String("gestalt.provider.v1"),
		Syntax:  goproto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: goproto.String("Subject"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:   goproto.String("type"),
				Number: goproto.Int32(1),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
			}},
		}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: goproto.String("AuthorizationProvider"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       goproto.String("CheckAccess"),
				InputType:  goproto.String(".gestalt.provider.v1.Subject"),
				OutputType: goproto.String(".gestalt.provider.v1.Subject"),
			}},
		}},
		SourceCodeInfo: &descriptorpb.SourceCodeInfo{Location: []*descriptorpb.SourceCodeInfo_Location{
			{Path: []int32{4, 0}, Span: []int32{0, 0, 0}, LeadingComments: goproto.String(" Subject docs.\n")},
			{Path: []int32{4, 0, 2, 0}, Span: []int32{1, 0, 0}, LeadingComments: goproto.String(" Type docs.\n")},
			{Path: []int32{6, 0}, Span: []int32{2, 0, 0}, LeadingComments: goproto.String(" Service docs.\n")},
			{Path: []int32{6, 0, 2, 0}, Span: []int32{3, 0, 0}, LeadingComments: goproto.String(" Method docs.\n")},
		}},
	}
	fd, err := protodesc.NewFile(file, nil)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	svc := fd.Services().ByName("AuthorizationProvider")
	ir, err := buildProviderSDKIR(authorizationTestServiceConfig(), svc)
	if err != nil {
		t.Fatalf("buildProviderSDKIR: %v", err)
	}
	if ir.Doc != "Service docs." {
		t.Fatalf("service doc = %q", ir.Doc)
	}
	if ir.MessagesByName["Subject"].Doc != "Subject docs." {
		t.Fatalf("message doc = %q", ir.MessagesByName["Subject"].Doc)
	}
	if got := ir.MessagesByName["Subject"].Fields[0].Doc; got != "Type docs." {
		t.Fatalf("field doc = %q", got)
	}
	if ir.Methods[0].Doc != "Method docs." {
		t.Fatalf("method doc = %q", ir.Methods[0].Doc)
	}
}

func TestWriteGeneratedFilesWritesExpectedOutputs(t *testing.T) {
	t.Parallel()

	cfg := config{
		Version:  1,
		Services: []serviceConfig{authorizationTestServiceConfig()},
	}
	services, err := validateImage(cfg, fileDescriptorSet(protov1.File_v1_authorization_proto))
	if err != nil {
		t.Fatalf("validateImage: %v", err)
	}
	root := t.TempDir()
	files, err := writeGeneratedFiles(cfg, services, root)
	if err != nil {
		t.Fatalf("writeGeneratedFiles: %v", err)
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil {
			t.Fatalf("read generated %s: %v", file.Path, err)
		}
		if !bytes.Equal(data, file.Data) {
			t.Fatalf("generated %s did not match rendered bytes", file.Path)
		}
	}
	sort.Strings(paths)
	want := []string{
		"sdk/go/authorization/client.go",
		"sdk/go/authorization/conversions.go",
		"sdk/go/authorization/doc.go",
		"sdk/go/authorization/protocol.go",
		"sdk/go/authorization/types.go",
		"sdk/python/gestalt/authorization.py",
		"sdk/rust/src/authorization.rs",
		"sdk/typescript/src/authorization.ts",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("generated paths:\n%s\nwant:\n%s", strings.Join(paths, "\n"), strings.Join(want, "\n"))
	}
}

func cloneFileDescriptorSet(image *descriptorpb.FileDescriptorSet) *descriptorpb.FileDescriptorSet {
	return goproto.Clone(image).(*descriptorpb.FileDescriptorSet)
}

func authorizationField(t *testing.T, image *descriptorpb.FileDescriptorSet, messageName, fieldName string) *descriptorpb.FieldDescriptorProto {
	t.Helper()

	for _, file := range image.GetFile() {
		if file.GetName() != "v1/authorization.proto" {
			continue
		}
		for _, message := range file.GetMessageType() {
			if message.GetName() != messageName {
				continue
			}
			for _, field := range message.GetField() {
				if field.GetName() == fieldName {
					return field
				}
			}
		}
	}
	t.Fatalf("field %s.%s not found", messageName, fieldName)
	return nil
}

func authorizationTestServiceConfig() serviceConfig {
	return serviceConfig{
		Proto:   "gestalt.provider.v1.AuthorizationProvider",
		SDKName: "Authorization",
		Package: map[string]string{
			"typescript": "Authorization",
			"python":     "authorization",
			"go":         "authorization",
			"rust":       "authorization",
		},
	}
}

func exampleTestServiceConfig() serviceConfig {
	return serviceConfig{
		Proto:   "gestalt.provider.v1.ExampleProvider",
		SDKName: "Example",
		Package: map[string]string{
			"typescript": "Example",
			"python":     "example",
			"go":         "example",
			"rust":       "example",
		},
	}
}

func assertFieldPresence(t *testing.T, message irMessage, protoName string, presence irPresence, kind irFieldKind) {
	t.Helper()

	for _, field := range message.Fields {
		if field.ProtoName != protoName {
			continue
		}
		if field.Presence != presence || field.Kind != kind {
			t.Fatalf("%s.%s = presence %s kind %s, want presence %s kind %s", message.ProtoName, protoName, field.Presence, field.Kind, presence, kind)
		}
		return
	}
	t.Fatalf("%s.%s not found", message.ProtoName, protoName)
}
