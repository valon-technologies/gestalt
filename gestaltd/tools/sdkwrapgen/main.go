package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	Proto   string            `yaml:"proto"`
	SDKName string            `yaml:"sdk_name"`
	Package map[string]string `yaml:"package"`
}

type generatedFile struct {
	Path string
	Data []byte
}

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
	if err := validateCLIInputs(*imagePath, *outRoot, *printIR); err != nil {
		exit(err)
	}
	var services []protoreflect.ServiceDescriptor
	if *imagePath != "" {
		image, err := readImage(*imagePath)
		if err != nil {
			exit(err)
		}
		services, err = validateImage(cfg, image)
		if err != nil {
			exit(err)
		}
		if *printIR {
			for _, svc := range services {
				fmt.Printf("%s\n", svc.FullName())
			}
		}
	}
	if *outRoot != "" {
		if _, err := writeGeneratedFiles(cfg, services, *outRoot); err != nil {
			exit(err)
		}
	}
}

func validateCLIInputs(imagePath, outRoot string, printIR bool) error {
	if imagePath == "" && (outRoot != "" || printIR) {
		return fmt.Errorf("-image is required when writing generated files or printing IR")
	}
	return nil
}

func readConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
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
		if svc.SDKName == "" {
			return fmt.Errorf("%s: sdk_name is required", svc.Proto)
		}
		for _, lang := range []string{"typescript", "python", "go", "rust"} {
			if svc.Package[lang] == "" {
				return fmt.Errorf("%s: missing %s package name", svc.Proto, lang)
			}
		}
	}
	return nil
}

func writeGeneratedFiles(cfg config, services []protoreflect.ServiceDescriptor, root string) ([]generatedFile, error) {
	files, err := renderGeneratedFiles(cfg, services)
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

func renderGeneratedFiles(cfg config, services []protoreflect.ServiceDescriptor) ([]generatedFile, error) {
	files := []generatedFile{}
	seen := map[string]bool{}
	if len(services) != len(cfg.Services) {
		return nil, fmt.Errorf("validated services len = %d, want %d", len(services), len(cfg.Services))
	}
	for svcIndex, svc := range cfg.Services {
		ir, err := buildProviderSDKIR(svc, services[svcIndex])
		if err != nil {
			return nil, err
		}
		for _, lang := range providerSDKLanguages() {
			rendered, err := renderProviderSDKLanguage(ir, lang)
			if err != nil {
				return nil, err
			}
			for _, file := range rendered {
				if file.Path == "" {
					return nil, fmt.Errorf("%s: %s output path is required", svc.Proto, lang)
				}
				if seen[file.Path] {
					return nil, fmt.Errorf("duplicate sdk output %q", file.Path)
				}
				seen[file.Path] = true
				file.Data = normalizeGeneratedData(file.Data)
				files = append(files, file)
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func providerSDKLanguages() []string {
	return []string{"go", "python", "rust", "typescript"}
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

func generatedHeader(path string) []byte {
	prefix := "//"
	if strings.HasSuffix(path, ".py") {
		prefix = "#"
	}
	return []byte(fmt.Sprintf("%s Code generated by gestaltd/tools/sdkwrapgen. DO NOT EDIT.\n\n", prefix))
}

func normalizeGeneratedData(data []byte) []byte {
	return []byte(strings.TrimRight(string(data), "\n") + "\n")
}

func readImage(path string) (*descriptorpb.FileDescriptorSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	binaryImage := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, binaryImage); err != nil {
		textImage := &descriptorpb.FileDescriptorSet{}
		if textErr := prototext.Unmarshal(data, textImage); textErr != nil {
			return nil, fmt.Errorf("parse image: %w", err)
		}
		return textImage, nil
	}
	return binaryImage, nil
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
		found = append(found, service)
	}
	return found, nil
}

func validateServiceContract(cfg serviceConfig, service protoreflect.ServiceDescriptor) error {
	seen := map[protoreflect.FullName]bool{}
	methods := service.Methods()
	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		if err := validateMessageContract(method.Input(), seen); err != nil {
			return fmt.Errorf("%s input %s: %w", service.FullName(), method.Name(), err)
		}
		if err := validateMessageContract(method.Output(), seen); err != nil {
			return fmt.Errorf("%s output %s: %w", service.FullName(), method.Name(), err)
		}
	}
	return nil
}

func validateMessageContract(message protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) error {
	if message == nil {
		return nil
	}
	if seen[message.FullName()] {
		return nil
	}
	seen[message.FullName()] = true

	oneofs := message.Oneofs()
	if oneofs.Len() > 1 {
		return fmt.Errorf("%s has %d oneofs; only one oneof per message is supported", message.FullName(), oneofs.Len())
	}

	fields := message.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.IsMap() {
			return fmt.Errorf("%s.%s map fields are not supported; use google.protobuf.Struct for JSON objects", message.FullName(), field.Name())
		}
		if !supportedFieldKind(field.Kind()) {
			return fmt.Errorf("%s.%s has unsupported field kind %s", message.FullName(), field.Name(), field.Kind())
		}
		if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
			continue
		}
		fieldMessage := field.Message()
		switch fieldMessage.FullName() {
		case "google.protobuf.Struct", "google.protobuf.Timestamp":
		default:
			if err := validateMessageContract(fieldMessage, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func supportedFieldKind(kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.StringKind,
		protoreflect.BoolKind,
		protoreflect.Int32Kind,
		protoreflect.EnumKind,
		protoreflect.MessageKind,
		protoreflect.GroupKind:
		return true
	default:
		return false
	}
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

func exit(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
