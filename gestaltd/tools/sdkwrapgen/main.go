package main

import (
	"flag"
	"fmt"
	"os"

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
}

type wellKnownTypeRule struct {
	Semantic string `yaml:"semantic"`
}

func main() {
	configPath := flag.String("config", "../../sdk/sdkgen.yaml", "SDK generator config path")
	imagePath := flag.String("image", "", "Buf image descriptor path")
	printIR := flag.Bool("print-ir", false, "print validated service descriptors")
	flag.Parse()

	cfg, err := readConfig(*configPath)
	if err != nil {
		exit(err)
	}
	if err := validateConfig(cfg); err != nil {
		exit(err)
	}
	if *imagePath == "" {
		return
	}
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

func exit(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
