package main

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

type ProviderSDKIR struct {
	Config         serviceConfig
	ProtoPackage   string
	ProtoFileBase  string
	ServiceName    string
	Doc            string
	Messages       []irMessage
	MessagesByName map[string]irMessage
	Enums          []irEnum
	EnumsByName    map[string]irEnum
	Methods        []irMethod
}

type irMessage struct {
	ProtoName     string
	FullName      string
	PublicName    string
	Doc           string
	Fields        []irField
	Oneof         *irOneof
	Empty         bool
	ProtoGoName   string
	ProtoRustName string
}

type irField struct {
	ProtoName       string
	JSONName        string
	Number          int32
	GoName          string
	ProtoGoName     string
	PyName          string
	RustName        string
	Kind            irFieldKind
	Presence        irPresence
	Doc             string
	Repeated        bool
	MessageName     string
	EnumName        string
	DefaultValue    string
	DefaultEnumName string
	Oneof           bool
}

type irFieldKind string

const (
	irKindString    irFieldKind = "string"
	irKindBool      irFieldKind = "bool"
	irKindInt32     irFieldKind = "int32"
	irKindEnum      irFieldKind = "enum"
	irKindMessage   irFieldKind = "message"
	irKindJSON      irFieldKind = "json_object"
	irKindTimestamp irFieldKind = "timestamp"
)

type irPresence string

const (
	irPresenceNone     irPresence = "none"
	irPresenceMessage  irPresence = "message"
	irPresenceRepeated irPresence = "repeated"
	irPresenceOneof    irPresence = "oneof"
)

type irOneof struct {
	ProtoName string
	Doc       string
	Variants  []irField
}

type irEnum struct {
	ProtoName string
	Doc       string
	Values    []irEnumValue
}

type irEnumValue struct {
	ProtoName  string
	PublicName string
	GoName     string
	RustName   string
	Doc        string
	Number     int32
}

type irMethod struct {
	ProtoName  string
	LowerName  string
	SnakeName  string
	Doc        string
	InputName  string
	OutputName string
	EmptyInput bool
	HumanLabel string
}

func buildProviderSDKIR(cfg serviceConfig, service protoreflect.ServiceDescriptor) (ProviderSDKIR, error) {
	file := service.ParentFile()
	ir := ProviderSDKIR{
		Config:         cfg,
		ProtoPackage:   string(file.Package()),
		ProtoFileBase:  protoFileBase(file.Path()),
		ServiceName:    string(service.Name()),
		Doc:            descriptorDoc(service),
		Messages:       make([]irMessage, 0, file.Messages().Len()),
		MessagesByName: map[string]irMessage{},
		Enums:          make([]irEnum, 0, file.Enums().Len()),
		EnumsByName:    map[string]irEnum{},
		Methods:        make([]irMethod, 0, service.Methods().Len()),
	}

	reachableMessages, reachableEnums := collectReachableDescriptors(service)
	for i := 0; i < file.Enums().Len(); i++ {
		descriptor := file.Enums().Get(i)
		if !reachableEnums[descriptor.FullName()] {
			continue
		}
		enum := buildIREnum(descriptor)
		ir.Enums = append(ir.Enums, enum)
		ir.EnumsByName[enum.ProtoName] = enum
	}
	sort.Slice(ir.Enums, func(i, j int) bool { return ir.Enums[i].ProtoName < ir.Enums[j].ProtoName })

	for i := 0; i < file.Messages().Len(); i++ {
		descriptor := file.Messages().Get(i)
		if !reachableMessages[descriptor.FullName()] {
			continue
		}
		message := buildIRMessage(cfg, descriptor)
		ir.Messages = append(ir.Messages, message)
		ir.MessagesByName[message.ProtoName] = message
	}

	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		input := protoMessageName(method.Input())
		output := protoMessageName(method.Output())
		ir.Methods = append(ir.Methods, irMethod{
			ProtoName:  string(method.Name()),
			LowerName:  lowerFirst(string(method.Name())),
			SnakeName:  snakeName(string(method.Name())),
			Doc:        descriptorDoc(method),
			InputName:  input,
			OutputName: output,
			EmptyInput: isEmptyDescriptor(method.Input()),
			HumanLabel: humanMethodLabel(string(method.Name())),
		})
	}
	return ir, nil
}

func collectReachableDescriptors(service protoreflect.ServiceDescriptor) (map[protoreflect.FullName]bool, map[protoreflect.FullName]bool) {
	messages := map[protoreflect.FullName]bool{}
	enums := map[protoreflect.FullName]bool{}
	var visitMessage func(protoreflect.MessageDescriptor)
	visitMessage = func(message protoreflect.MessageDescriptor) {
		if message == nil || isEmptyDescriptor(message) || messages[message.FullName()] {
			return
		}
		messages[message.FullName()] = true
		fields := message.Fields()
		for i := 0; i < fields.Len(); i++ {
			field := fields.Get(i)
			switch field.Kind() {
			case protoreflect.EnumKind:
				enums[field.Enum().FullName()] = true
			case protoreflect.MessageKind, protoreflect.GroupKind:
				switch field.Message().FullName() {
				case "google.protobuf.Struct", "google.protobuf.Timestamp":
				default:
					visitMessage(field.Message())
				}
			}
		}
	}
	for i := 0; i < service.Methods().Len(); i++ {
		method := service.Methods().Get(i)
		visitMessage(method.Input())
		visitMessage(method.Output())
	}
	return messages, enums
}

func renderProviderSDKLanguage(ir ProviderSDKIR, lang string) ([]generatedFile, error) {
	switch lang {
	case "typescript":
		return renderTypeScriptProviderSDK(ir)
	case "python":
		return renderPythonProviderSDK(ir)
	case "go":
		return renderGoProviderSDK(ir)
	case "rust":
		return renderRustProviderSDK(ir)
	default:
		return nil, fmt.Errorf("%s: unsupported output language %q", ir.Config.Proto, lang)
	}
}

func buildIRMessage(cfg serviceConfig, message protoreflect.MessageDescriptor) irMessage {
	out := irMessage{
		ProtoName:     string(message.Name()),
		FullName:      string(message.FullName()),
		PublicName:    publicMessageName(cfg, string(message.Name())),
		Doc:           descriptorDoc(message),
		Empty:         isEmptyDescriptor(message),
		ProtoGoName:   string(message.Name()),
		ProtoRustName: rustTypeName(string(message.Name())),
	}
	fields := message.Fields()
	var oneof *irOneof
	if message.Oneofs().Len() > 0 {
		descriptor := message.Oneofs().Get(0)
		oneof = &irOneof{ProtoName: string(descriptor.Name()), Doc: descriptorDoc(descriptor)}
	}
	for i := 0; i < fields.Len(); i++ {
		field := buildIRField(fields.Get(i))
		if field.Oneof {
			oneof.Variants = append(oneof.Variants, field)
			continue
		}
		out.Fields = append(out.Fields, field)
	}
	if oneof != nil {
		out.Oneof = oneof
	}
	return out
}

func buildIRField(field protoreflect.FieldDescriptor) irField {
	out := irField{
		ProtoName:   string(field.Name()),
		JSONName:    field.JSONName(),
		Number:      int32(field.Number()),
		GoName:      goFieldName(string(field.Name())),
		ProtoGoName: protoGoFieldName(string(field.Name())),
		PyName:      string(field.Name()),
		RustName:    rustFieldName(string(field.Name())),
		Doc:         descriptorDoc(field),
		Repeated:    field.IsList(),
		Oneof:       field.ContainingOneof() != nil,
	}
	out.Presence = fieldPresence(field)
	switch field.Kind() {
	case protoreflect.StringKind:
		out.Kind = irKindString
		out.DefaultValue = `""`
	case protoreflect.BoolKind:
		out.Kind = irKindBool
		out.DefaultValue = "false"
	case protoreflect.Int32Kind:
		out.Kind = irKindInt32
		out.DefaultValue = "0"
	case protoreflect.EnumKind:
		out.Kind = irKindEnum
		out.EnumName = string(field.Enum().Name())
		if field.Enum().Values().Len() > 0 {
			out.DefaultEnumName = strings.TrimPrefix(string(field.Enum().Values().Get(0).Name()), enumValuePrefix(out.EnumName))
		}
		out.DefaultValue = "0"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		switch field.Message().FullName() {
		case "google.protobuf.Struct":
			out.Kind = irKindJSON
		case "google.protobuf.Timestamp":
			out.Kind = irKindTimestamp
		default:
			out.Kind = irKindMessage
			out.MessageName = protoMessageName(field.Message())
		}
	default:
		out.Kind = irKindInt32
		out.DefaultValue = "0"
	}
	return out
}

func buildIREnum(enum protoreflect.EnumDescriptor) irEnum {
	out := irEnum{ProtoName: string(enum.Name()), Doc: descriptorDoc(enum)}
	prefix := enumValuePrefix(out.ProtoName)
	for i := 0; i < enum.Values().Len(); i++ {
		value := enum.Values().Get(i)
		public := strings.TrimPrefix(string(value.Name()), prefix)
		out.Values = append(out.Values, irEnumValue{
			ProtoName:  string(value.Name()),
			PublicName: public,
			GoName:     goEnumValueName(out.ProtoName, public),
			RustName:   rustEnumValueName(public),
			Doc:        descriptorDoc(value),
			Number:     int32(value.Number()),
		})
	}
	return out
}

func fieldPresence(field protoreflect.FieldDescriptor) irPresence {
	if field.ContainingOneof() != nil {
		return irPresenceOneof
	}
	if field.IsList() {
		return irPresenceRepeated
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		return irPresenceMessage
	}
	return irPresenceNone
}

func descriptorDoc(descriptor protoreflect.Descriptor) string {
	location := descriptor.ParentFile().SourceLocations().ByDescriptor(descriptor)
	return strings.TrimSpace(location.LeadingComments)
}

func protoMessageName(message protoreflect.MessageDescriptor) string {
	if message == nil {
		return ""
	}
	return string(message.Name())
}

func isEmptyDescriptor(message protoreflect.MessageDescriptor) bool {
	return message != nil && message.FullName() == "google.protobuf.Empty"
}

func publicMessageName(cfg serviceConfig, name string) string {
	if strings.HasPrefix(name, cfg.SDKName) && len(name) > len(cfg.SDKName) {
		return strings.TrimPrefix(name, cfg.SDKName)
	}
	return name
}

func goFieldName(name string) string {
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if strings.EqualFold(part, "id") {
			parts[i] = "ID"
			continue
		}
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func protoGoFieldName(name string) string {
	return camelName(name)
}

func goEnumValueName(enumName, valueName string) string {
	return enumName + camelName(strings.ToLower(valueName))
}

func rustEnumValueName(valueName string) string {
	return camelName(strings.ToLower(valueName))
}

func rustTypeName(name string) string {
	return name
}

func rustFieldName(name string) string {
	if name == "type" {
		return "r#type"
	}
	return name
}

func camelName(value string) string {
	parts := strings.Split(value, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func snakeName(value string) string {
	var out []rune
	for i, r := range value {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, '_')
		}
		out = append(out, r)
	}
	return strings.ToLower(string(out))
}

func protoFileBase(path string) string {
	base := path
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	return strings.TrimSuffix(base, ".proto")
}
