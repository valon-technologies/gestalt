package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"gopkg.in/yaml.v3"
)

type config struct {
	Version  int             `yaml:"version"`
	Services []serviceConfig `yaml:"services"`
}

type serviceConfig struct {
	Proto          string                       `yaml:"proto"`
	SDKName        string                       `yaml:"sdk_name"`
	Package        map[string]string            `yaml:"package"`
	WellKnownTypes map[string]wellKnownTypeRule `yaml:"well_known_types"`
	EnumPolicy     string                       `yaml:"enum_policy"`
	OneofPolicy    string                       `yaml:"oneof_policy"`
	Outputs        map[string][]outputConfig    `yaml:"outputs"`
}

type wellKnownTypeRule struct {
	Semantic string `yaml:"semantic"`
}

type outputConfig struct {
	Path     string `yaml:"path"`
	Template string `yaml:"template"`
}

type generatedFile struct {
	Path string
	Data []byte
}

//go:embed templates/*
var templates embed.FS

func main() {
	configPath := flag.String("config", "../../sdk/sdkgen.yaml", "SDK generator config path")
	imagePath := flag.String("image", "", "Buf image descriptor path")
	outRoot := flag.String("out-root", "", "write generated SDK files under this repository root")
	printIR := flag.Bool("print-ir", false, "print validated service descriptors")
	flag.Parse()

	cfg, err := readConfig(*configPath)
	if err != nil {
		exit(err)
	}
	if err := validateConfig(cfg); err != nil {
		exit(err)
	}
	if *imagePath != "" {
		image, err := readImage(*imagePath)
		if err != nil {
			exit(err)
		}
		services, err := validateImage(cfg, image)
		if err != nil {
			exit(err)
		}
		if *printIR {
			for _, svc := range services {
				fmt.Printf("%s\n", svc.FullName())
			}
		}
	} else if *outRoot != "" {
		if _, err := validateImage(cfg, fileDescriptorSet(protov1.File_v1_authorization_proto)); err != nil {
			exit(err)
		}
	}
	if *outRoot != "" {
		if _, err := writeGeneratedFiles(cfg, *outRoot); err != nil {
			exit(err)
		}
	}
}

func readConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported sdkgen config version %d", cfg.Version)
	}
	if len(cfg.Services) == 0 {
		return fmt.Errorf("sdkgen config must select at least one service")
	}
	seen := map[string]bool{}
	for _, svc := range cfg.Services {
		if svc.Proto == "" {
			return fmt.Errorf("sdkgen service proto name is required")
		}
		if seen[svc.Proto] {
			return fmt.Errorf("duplicate sdkgen service %q", svc.Proto)
		}
		seen[svc.Proto] = true
		if svc.EnumPolicy != "preserve_unknown_numeric" {
			return fmt.Errorf("%s: unsupported enum policy %q", svc.Proto, svc.EnumPolicy)
		}
		if svc.OneofPolicy != "explicit_variants" {
			return fmt.Errorf("%s: unsupported oneof policy %q", svc.Proto, svc.OneofPolicy)
		}
		for _, lang := range []string{"typescript", "python", "go", "rust"} {
			if svc.Package[lang] == "" {
				return fmt.Errorf("%s: missing %s package name", svc.Proto, lang)
			}
			if len(svc.Outputs[lang]) == 0 {
				return fmt.Errorf("%s: missing %s outputs", svc.Proto, lang)
			}
		}
		if svc.WellKnownTypes["google.protobuf.Struct"].Semantic != "json_object" {
			return fmt.Errorf("%s: google.protobuf.Struct must use json_object semantics", svc.Proto)
		}
		if svc.WellKnownTypes["google.protobuf.Timestamp"].Semantic != "timestamp_with_presence" {
			return fmt.Errorf("%s: google.protobuf.Timestamp must use timestamp_with_presence semantics", svc.Proto)
		}
	}
	return nil
}

func writeGeneratedFiles(cfg config, root string) ([]generatedFile, error) {
	files, err := renderGeneratedFiles(cfg)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		path, err := outputPath(root, file.Path)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create output directory for %s: %w", file.Path, err)
		}
		if err := os.WriteFile(path, file.Data, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	return files, nil
}

func renderGeneratedFiles(cfg config) ([]generatedFile, error) {
	files := []generatedFile{}
	seen := map[string]bool{}
	for _, svc := range cfg.Services {
		if svc.SDKName != "Authorization" {
			return nil, fmt.Errorf("%s: unsupported sdk name %q", svc.Proto, svc.SDKName)
		}
		for _, lang := range sortedOutputLanguages(svc.Outputs) {
			for _, out := range svc.Outputs[lang] {
				if out.Path == "" {
					return nil, fmt.Errorf("%s: %s output path is required", svc.Proto, lang)
				}
				if out.Template == "" {
					return nil, fmt.Errorf("%s: %s output template is required for %s", svc.Proto, lang, out.Path)
				}
				if seen[out.Path] {
					return nil, fmt.Errorf("duplicate sdk output %q", out.Path)
				}
				seen[out.Path] = true
				body, err := templates.ReadFile("templates/" + cleanTemplatePath(out.Template))
				if err != nil {
					return nil, fmt.Errorf("read template %s: %w", out.Template, err)
				}
				files = append(files, generatedFile{
					Path: out.Path,
					Data: append(generatedHeader(out.Path), body...),
				})
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func sortedOutputLanguages(outputs map[string][]outputConfig) []string {
	langs := make([]string, 0, len(outputs))
	for lang := range outputs {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

func outputPath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("invalid relative output path %q", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("invalid relative output path %q", rel)
	}
	return filepath.Join(root, clean), nil
}

func cleanTemplatePath(path string) string {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return ""
	}
	return clean
}

func generatedHeader(path string) []byte {
	prefix := "//"
	if strings.HasSuffix(path, ".py") {
		prefix = "#"
	}
	return []byte(fmt.Sprintf("%s Code generated by gestaltd/tools/sdkwrapgen. DO NOT EDIT.\n\n", prefix))
}

func readImage(path string) (*descriptorpb.FileDescriptorSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	image := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, image); err != nil {
		if textErr := prototext.Unmarshal(data, image); textErr != nil {
			return nil, fmt.Errorf("parse image: %w", err)
		}
	}
	return image, nil
}

func validateImage(cfg config, image *descriptorpb.FileDescriptorSet) ([]protoreflect.ServiceDescriptor, error) {
	found := make([]protoreflect.ServiceDescriptor, 0, len(cfg.Services))
	for _, selected := range cfg.Services {
		service, err := findService(image, protoreflect.FullName(selected.Proto))
		if err != nil {
			return nil, err
		}
		if service == nil {
			return nil, fmt.Errorf("service %q not found in descriptor image", selected.Proto)
		}
		if err := validateServiceContract(selected, service); err != nil {
			return nil, err
		}
		if selected.SDKName == "Authorization" {
			if err := validateAuthorizationExactContract(service); err != nil {
				return nil, err
			}
		}
		found = append(found, service)
	}
	return found, nil
}

func validateServiceContract(cfg serviceConfig, service protoreflect.ServiceDescriptor) error {
	seen := map[protoreflect.FullName]bool{}
	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		if err := validateMessageContract(cfg, method.Input(), seen); err != nil {
			return fmt.Errorf("%s input %s: %w", service.FullName(), method.Name(), err)
		}
		if err := validateMessageContract(cfg, method.Output(), seen); err != nil {
			return fmt.Errorf("%s output %s: %w", service.FullName(), method.Name(), err)
		}
	}
	return nil
}

func validateMessageContract(cfg serviceConfig, message protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) error {
	if message == nil {
		return nil
	}
	if seen[message.FullName()] {
		return nil
	}
	seen[message.FullName()] = true

	oneofs := message.Oneofs()
	for i := 0; i < oneofs.Len(); i++ {
		if cfg.OneofPolicy != "explicit_variants" {
			return fmt.Errorf("%s oneof %s requires explicit_variants policy", message.FullName(), oneofs.Get(i).Name())
		}
	}

	fields := message.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.Kind() == protoreflect.EnumKind && cfg.EnumPolicy != "preserve_unknown_numeric" {
			return fmt.Errorf("%s.%s enum requires preserve_unknown_numeric policy", message.FullName(), field.Name())
		}
		if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
			continue
		}
		fieldMessage := field.Message()
		switch fieldMessage.FullName() {
		case "google.protobuf.Struct":
			if cfg.WellKnownTypes["google.protobuf.Struct"].Semantic != "json_object" {
				return fmt.Errorf("%s.%s Struct field requires json_object semantics", message.FullName(), field.Name())
			}
		case "google.protobuf.Timestamp":
			if cfg.WellKnownTypes["google.protobuf.Timestamp"].Semantic != "timestamp_with_presence" {
				return fmt.Errorf("%s.%s Timestamp field requires timestamp_with_presence semantics", message.FullName(), field.Name())
			}
		default:
			if err := validateMessageContract(cfg, fieldMessage, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func findService(image *descriptorpb.FileDescriptorSet, name protoreflect.FullName) (protoreflect.ServiceDescriptor, error) {
	files, err := protodesc.NewFiles(image)
	if err != nil {
		return nil, fmt.Errorf("parse descriptor image: %w", err)
	}
	for _, file := range image.GetFile() {
		fd, err := files.FindFileByPath(file.GetName())
		if err != nil {
			continue
		}
		services := fd.Services()
		for i := 0; i < services.Len(); i++ {
			service := services.Get(i)
			if service.FullName() == name {
				return service, nil
			}
		}
	}
	return nil, nil
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

func validateAuthorizationExactContract(service protoreflect.ServiceDescriptor) error {
	expectedMethods := []string{
		"CheckAccess:gestalt.provider.v1.CheckAccessRequest:gestalt.provider.v1.CheckAccessResponse",
		"CheckAccessMany:gestalt.provider.v1.CheckAccessManyRequest:gestalt.provider.v1.CheckAccessManyResponse",
		"ListRelationships:gestalt.provider.v1.ListRelationshipsRequest:gestalt.provider.v1.ListRelationshipsResponse",
		"AddRelationship:gestalt.provider.v1.AddRelationshipRequest:gestalt.provider.v1.AddRelationshipResponse",
		"DeleteRelationship:gestalt.provider.v1.DeleteRelationshipRequest:gestalt.provider.v1.DeleteRelationshipResponse",
		"SetAuthorizationState:gestalt.provider.v1.SetAuthorizationStateRequest:gestalt.provider.v1.SetAuthorizationStateResponse",
		"GetActiveModelRef:google.protobuf.Empty:gestalt.provider.v1.GetActiveModelRefResponse",
		"SetActiveModel:gestalt.provider.v1.SetActiveModelRequest:gestalt.provider.v1.SetActiveModelResponse",
		"ListActiveModelResourceTypes:gestalt.provider.v1.ListActiveModelResourceTypesRequest:gestalt.provider.v1.ListActiveModelResourceTypesResponse",
	}
	methods := make([]string, 0, service.Methods().Len())
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		methods = append(methods, fmt.Sprintf("%s:%s:%s", method.Name(), method.Input().FullName(), method.Output().FullName()))
	}
	if err := compareStrings("authorization methods", methods, expectedMethods); err != nil {
		return err
	}

	seen := map[protoreflect.FullName]bool{}
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		if err := validateAuthorizationMessage(method.Input(), seen); err != nil {
			return err
		}
		if err := validateAuthorizationMessage(method.Output(), seen); err != nil {
			return err
		}
	}

	if err := validateAuthorizationEnums(service.ParentFile()); err != nil {
		return err
	}
	return nil
}

func validateAuthorizationMessage(message protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) error {
	if message == nil || isWellKnownMessage(message.FullName()) || seen[message.FullName()] {
		return nil
	}
	seen[message.FullName()] = true
	expected, ok := authorizationMessageFields[string(message.FullName())]
	if !ok {
		return fmt.Errorf("%s: missing generated Authorization message contract", message.FullName())
	}
	fields := make([]string, 0, message.Fields().Len())
	for i := 0; i < message.Fields().Len(); i++ {
		field := message.Fields().Get(i)
		fields = append(fields, fieldSignature(field))
		if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
			if err := validateAuthorizationMessage(field.Message(), seen); err != nil {
				return err
			}
		}
	}
	if err := compareStrings(string(message.FullName()), fields, expected); err != nil {
		return err
	}
	return nil
}

func validateAuthorizationEnums(file protoreflect.FileDescriptor) error {
	expected := map[string][]string{
		"gestalt.provider.v1.RelationshipTargetType": {
			"RELATIONSHIP_TARGET_TYPE_UNSPECIFIED:0",
			"RELATIONSHIP_TARGET_TYPE_SUBJECT:1",
			"RELATIONSHIP_TARGET_TYPE_RESOURCE:2",
			"RELATIONSHIP_TARGET_TYPE_SUBJECT_SET:3",
		},
		"gestalt.provider.v1.DefaultAccessPolicy": {
			"DEFAULT_ACCESS_POLICY_DENY:0",
			"DEFAULT_ACCESS_POLICY_ALLOW:1",
		},
		"gestalt.provider.v1.SourceLayer": {
			"SOURCE_LAYER_UNSPECIFIED:0",
			"SOURCE_LAYER_STATIC_CONFIG:1",
			"SOURCE_LAYER_RUNTIME:2",
		},
	}
	for i := 0; i < file.Enums().Len(); i++ {
		enum := file.Enums().Get(i)
		want, ok := expected[string(enum.FullName())]
		if !ok {
			continue
		}
		values := make([]string, 0, enum.Values().Len())
		for j := 0; j < enum.Values().Len(); j++ {
			value := enum.Values().Get(j)
			values = append(values, fmt.Sprintf("%s:%d", value.Name(), value.Number()))
		}
		if err := compareStrings(string(enum.FullName()), values, want); err != nil {
			return err
		}
		delete(expected, string(enum.FullName()))
	}
	if len(expected) != 0 {
		missing := make([]string, 0, len(expected))
		for name := range expected {
			missing = append(missing, name)
		}
		sort.Strings(missing)
		return fmt.Errorf("authorization enum contract missing descriptors: %s", strings.Join(missing, ", "))
	}
	return nil
}

func isWellKnownMessage(name protoreflect.FullName) bool {
	return name == "google.protobuf.Empty" ||
		name == "google.protobuf.Struct" ||
		name == "google.protobuf.Timestamp"
}

func fieldSignature(field protoreflect.FieldDescriptor) string {
	oneof := ""
	if field.ContainingOneof() != nil {
		oneof = string(field.ContainingOneof().Name())
	}
	typeName := ""
	switch field.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		typeName = string(field.Message().FullName())
	case protoreflect.EnumKind:
		typeName = string(field.Enum().FullName())
	}
	return fmt.Sprintf("%s:%d:%s:%s:%s:%s", field.Name(), field.Number(), field.Kind(), field.Cardinality(), typeName, oneof)
}

func compareStrings(label string, got, want []string) error {
	if strings.Join(got, "\n") == strings.Join(want, "\n") {
		return nil
	}
	return fmt.Errorf("%s contract mismatch\ngot:\n%s\nwant:\n%s", label, strings.Join(got, "\n"), strings.Join(want, "\n"))
}

var authorizationMessageFields = map[string][]string{
	"gestalt.provider.v1.Subject": {
		"type:1:string:optional::",
		"id:2:string:optional::",
		"properties:3:message:optional:google.protobuf.Struct:",
	},
	"gestalt.provider.v1.Action": {
		"name:1:string:optional::",
		"properties:2:message:optional:google.protobuf.Struct:",
	},
	"gestalt.provider.v1.Resource": {
		"type:1:string:optional::",
		"id:2:string:optional::",
		"properties:3:message:optional:google.protobuf.Struct:",
	},
	"gestalt.provider.v1.CheckAccessRequest": {
		"subject:1:message:optional:gestalt.provider.v1.Subject:",
		"action:2:message:optional:gestalt.provider.v1.Action:",
		"resource:3:message:optional:gestalt.provider.v1.Resource:",
	},
	"gestalt.provider.v1.CheckAccessResponse": {
		"allowed:1:bool:optional::",
		"model_id:2:string:optional::",
	},
	"gestalt.provider.v1.CheckAccessManyRequest": {
		"requests:1:message:repeated:gestalt.provider.v1.CheckAccessRequest:",
	},
	"gestalt.provider.v1.CheckAccessManyResponse": {
		"decisions:1:message:repeated:gestalt.provider.v1.CheckAccessResponse:",
	},
	"gestalt.provider.v1.ListRelationshipsRequest": {
		"filter:1:message:optional:gestalt.provider.v1.RelationshipFilter:",
		"page_size:2:int32:optional::",
		"page_token:3:string:optional::",
	},
	"gestalt.provider.v1.RelationshipFilter": {
		"target:1:message:optional:gestalt.provider.v1.RelationshipTarget:",
		"relation:2:string:optional::",
		"resource:3:message:optional:gestalt.provider.v1.Resource:",
		"target_type:4:enum:optional:gestalt.provider.v1.RelationshipTargetType:",
		"target_entity_type:5:string:optional::",
		"resource_type:6:string:optional::",
		"source_layer:7:enum:optional:gestalt.provider.v1.SourceLayer:",
	},
	"gestalt.provider.v1.ListRelationshipsResponse": {
		"relationships:1:message:repeated:gestalt.provider.v1.Relationship:",
		"next_page_token:2:string:optional::",
	},
	"gestalt.provider.v1.AddRelationshipRequest": {
		"relationship:1:message:optional:gestalt.provider.v1.Relationship:",
	},
	"gestalt.provider.v1.AddRelationshipResponse": {
		"relationship:1:message:optional:gestalt.provider.v1.Relationship:",
	},
	"gestalt.provider.v1.DeleteRelationshipRequest": {
		"relationship_tuple:1:message:optional:gestalt.provider.v1.RelationshipTuple:",
	},
	"gestalt.provider.v1.DeleteRelationshipResponse": {},
	"gestalt.provider.v1.SetAuthorizationStateRequest": {
		"model:1:message:optional:gestalt.provider.v1.AuthorizationModel:",
		"relationships:2:message:repeated:gestalt.provider.v1.Relationship:",
	},
	"gestalt.provider.v1.SetAuthorizationStateResponse": {
		"active_model:1:message:optional:gestalt.provider.v1.AuthorizationModelRef:",
	},
	"gestalt.provider.v1.Relationship": {
		"tuple:1:message:optional:gestalt.provider.v1.RelationshipTuple:",
		"properties:2:message:optional:google.protobuf.Struct:",
		"source_layer:3:enum:optional:gestalt.provider.v1.SourceLayer:",
	},
	"gestalt.provider.v1.RelationshipTuple": {
		"target:1:message:optional:gestalt.provider.v1.RelationshipTarget:",
		"relation:2:string:optional::",
		"resource:3:message:optional:gestalt.provider.v1.Resource:",
	},
	"gestalt.provider.v1.RelationshipTarget": {
		"subject:1:message:optional:gestalt.provider.v1.Subject:kind",
		"resource:2:message:optional:gestalt.provider.v1.Resource:kind",
		"subject_set:3:message:optional:gestalt.provider.v1.SubjectSet:kind",
	},
	"gestalt.provider.v1.SubjectSet": {
		"resource:1:message:optional:gestalt.provider.v1.Resource:",
		"relation:2:string:optional::",
	},
	"gestalt.provider.v1.AuthorizationModel": {
		"id:1:string:optional::",
		"version:2:string:optional::",
		"resource_types:3:message:repeated:gestalt.provider.v1.AuthorizationModelResourceType:",
	},
	"gestalt.provider.v1.AuthorizationModelResourceType": {
		"name:1:string:optional::",
		"relations:2:message:repeated:gestalt.provider.v1.ModelRelation:",
		"actions:3:message:repeated:gestalt.provider.v1.ModelAction:",
		"source_layer:4:enum:optional:gestalt.provider.v1.SourceLayer:",
		"default_access_policy:5:enum:optional:gestalt.provider.v1.DefaultAccessPolicy:",
	},
	"gestalt.provider.v1.ModelRelation": {
		"name:1:string:optional::",
		"allowed_targets:2:message:repeated:gestalt.provider.v1.ModelAllowedTarget:",
	},
	"gestalt.provider.v1.ModelAction": {
		"name:1:string:optional::",
		"relations:2:string:repeated::",
	},
	"gestalt.provider.v1.ModelAllowedTarget": {
		"subject_type:1:string:optional::kind",
		"resource_type:2:string:optional::kind",
		"subject_set_type:3:message:optional:gestalt.provider.v1.SubjectSetType:kind",
	},
	"gestalt.provider.v1.SubjectSetType": {
		"resource_type:1:string:optional::",
		"relation:2:string:optional::",
	},
	"gestalt.provider.v1.AuthorizationModelRef": {
		"id:1:string:optional::",
		"version:2:string:optional::",
		"created_at:3:message:optional:google.protobuf.Timestamp:",
	},
	"gestalt.provider.v1.GetActiveModelRefResponse": {
		"model:1:message:optional:gestalt.provider.v1.AuthorizationModelRef:",
	},
	"gestalt.provider.v1.SetActiveModelRequest": {
		"model:1:message:optional:gestalt.provider.v1.AuthorizationModel:",
	},
	"gestalt.provider.v1.SetActiveModelResponse": {
		"model:1:message:optional:gestalt.provider.v1.AuthorizationModelRef:",
	},
	"gestalt.provider.v1.ListActiveModelResourceTypesRequest": {
		"filter:1:message:optional:gestalt.provider.v1.AuthorizationModelResourceTypeFilter:",
		"page_size:2:int32:optional::",
		"page_token:3:string:optional::",
	},
	"gestalt.provider.v1.AuthorizationModelResourceTypeFilter": {
		"name:1:string:optional::",
		"source_layer:2:enum:optional:gestalt.provider.v1.SourceLayer:",
	},
	"gestalt.provider.v1.ListActiveModelResourceTypesResponse": {
		"resource_types:1:message:repeated:gestalt.provider.v1.AuthorizationModelResourceType:",
		"next_page_token:2:string:optional::",
		"model_id:3:string:optional::",
	},
}

func exit(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
