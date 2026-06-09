package main

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

type authorizationIR struct {
	Config         serviceConfig
	ProtoPackage   string
	ServiceName    string
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
	Fields        []irField
	Oneof         *irOneof
	Empty         bool
	ProtoGoName   string
	ProtoRustName string
}

type irField struct {
	ProtoName       string
	JSONName        string
	GoName          string
	ProtoGoName     string
	PyName          string
	RustName        string
	Kind            irFieldKind
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

type irOneof struct {
	ProtoName string
	Variants  []irField
}

type irEnum struct {
	ProtoName string
	Values    []irEnumValue
}

type irEnumValue struct {
	ProtoName  string
	PublicName string
	GoName     string
	RustName   string
	Number     int32
}

type irMethod struct {
	ProtoName  string
	LowerName  string
	SnakeName  string
	InputName  string
	OutputName string
	EmptyInput bool
	HumanLabel string
}

func buildAuthorizationIR(cfg serviceConfig, service protoreflect.ServiceDescriptor) (authorizationIR, error) {
	if cfg.SDKName != "Authorization" {
		return authorizationIR{}, fmt.Errorf("%s: unsupported sdk name %q", cfg.Proto, cfg.SDKName)
	}
	file := service.ParentFile()
	ir := authorizationIR{
		Config:         cfg,
		ProtoPackage:   string(file.Package()),
		ServiceName:    string(service.Name()),
		Messages:       make([]irMessage, 0, file.Messages().Len()),
		MessagesByName: map[string]irMessage{},
		Enums:          make([]irEnum, 0, file.Enums().Len()),
		EnumsByName:    map[string]irEnum{},
		Methods:        make([]irMethod, 0, service.Methods().Len()),
	}

	for i := 0; i < file.Enums().Len(); i++ {
		enum := buildIREnum(file.Enums().Get(i))
		ir.Enums = append(ir.Enums, enum)
		ir.EnumsByName[enum.ProtoName] = enum
	}
	sort.Slice(ir.Enums, func(i, j int) bool { return ir.Enums[i].ProtoName < ir.Enums[j].ProtoName })

	for i := 0; i < file.Messages().Len(); i++ {
		message := buildIRMessage(file.Messages().Get(i))
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
			InputName:  input,
			OutputName: output,
			EmptyInput: isEmptyDescriptor(method.Input()),
			HumanLabel: humanMethodLabel(string(method.Name())),
		})
	}
	return ir, nil
}

func renderAuthorizationLanguage(ir authorizationIR, lang string, outputs []outputConfig) ([]generatedFile, error) {
	switch lang {
	case "typescript":
		return renderTypeScriptAuthorization(ir, outputs)
	case "python":
		return renderPythonAuthorization(ir, outputs)
	case "go":
		return renderGoAuthorization(ir, outputs)
	case "rust":
		return renderRustAuthorization(ir, outputs)
	default:
		return nil, fmt.Errorf("%s: unsupported output language %q", ir.Config.Proto, lang)
	}
}

func buildIRMessage(message protoreflect.MessageDescriptor) irMessage {
	out := irMessage{
		ProtoName:     string(message.Name()),
		FullName:      string(message.FullName()),
		PublicName:    publicMessageName(string(message.Name())),
		Empty:         isEmptyDescriptor(message),
		ProtoGoName:   string(message.Name()),
		ProtoRustName: rustTypeName(string(message.Name())),
	}
	fields := message.Fields()
	var oneof *irOneof
	if message.Oneofs().Len() > 0 {
		oneof = &irOneof{ProtoName: string(message.Oneofs().Get(0).Name())}
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
		GoName:      goFieldName(string(field.Name())),
		ProtoGoName: protoGoFieldName(string(field.Name())),
		PyName:      string(field.Name()),
		RustName:    rustFieldName(string(field.Name())),
		Repeated:    field.IsList(),
		Oneof:       field.ContainingOneof() != nil,
	}
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
	out := irEnum{ProtoName: string(enum.Name())}
	prefix := enumValuePrefix(out.ProtoName)
	for i := 0; i < enum.Values().Len(); i++ {
		value := enum.Values().Get(i)
		public := strings.TrimPrefix(string(value.Name()), prefix)
		out.Values = append(out.Values, irEnumValue{
			ProtoName:  string(value.Name()),
			PublicName: public,
			GoName:     goEnumValueName(out.ProtoName, public),
			RustName:   rustEnumValueName(public),
			Number:     int32(value.Number()),
		})
	}
	return out
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

func publicMessageName(name string) string {
	switch name {
	case "AuthorizationModel":
		return "Model"
	case "AuthorizationModelResourceType":
		return "ModelResourceType"
	case "AuthorizationModelRef":
		return "ModelRef"
	case "AuthorizationModelResourceTypeFilter":
		return "ModelResourceTypeFilter"
	default:
		return name
	}
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
	switch name {
	case "AuthorizationModel":
		return "AuthorizationModel"
	case "AuthorizationModelResourceType":
		return "AuthorizationModelResourceType"
	case "AuthorizationModelRef":
		return "AuthorizationModelRef"
	default:
		return name
	}
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
