package main

import (
	"fmt"
	"strings"
)

func renderGoConversions(ir authorizationIR) string {
	var b strings.Builder
	b.WriteString(`package authorization

import (
	"fmt"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

`)
	for _, message := range ir.Messages {
		if message.Empty {
			continue
		}
		if message.Oneof != nil {
			renderGoOneofConversions(&b, ir, message)
			continue
		}
		renderGoMessageConversions(&b, ir, message)
	}
	renderGoSliceAndTimeHelpers(&b, ir)
	return b.String()
}

func renderGoMessageConversions(b *strings.Builder, ir authorizationIR, message irMessage) {
	b.WriteString(fmt.Sprintf("func %sFromProto(in *proto.%s) *%s {\n", message.PublicName, message.ProtoName, message.PublicName))
	b.WriteString("\tif in == nil {\n\t\treturn nil\n\t}\n")
	b.WriteString(fmt.Sprintf("\treturn &%s{\n", message.PublicName))
	for _, field := range message.Fields {
		b.WriteString(fmt.Sprintf("\t\t%s: %s,\n", field.GoName, goFromProtoFieldExpr(ir, field, "in")))
	}
	b.WriteString("\t}\n}\n\n")

	b.WriteString(fmt.Sprintf("func %sToProto(in *%s) (*proto.%s, error) {\n", message.PublicName, message.PublicName, message.ProtoName))
	b.WriteString("\tif in == nil {\n\t\treturn nil, nil\n\t}\n")
	for _, field := range message.Fields {
		if fieldNeedsGoLocal(field) {
			b.WriteString(goToProtoLocal(ir, field, "in."+field.GoName, field.JSONName))
		}
	}
	b.WriteString(fmt.Sprintf("\treturn &proto.%s{\n", message.ProtoName))
	for _, field := range message.Fields {
		b.WriteString(fmt.Sprintf("\t\t%s: %s,\n", field.ProtoGoName, goToProtoFieldExpr(ir, field, "in."+field.GoName, field.JSONName)))
	}
	b.WriteString("\t}, nil\n}\n\n")
}

func renderGoOneofConversions(b *strings.Builder, ir authorizationIR, message irMessage) {
	lower := lowerFirst(message.PublicName)
	b.WriteString(fmt.Sprintf("func %sFromProto(in *proto.%s) %s {\n", lower, message.ProtoName, message.PublicName))
	b.WriteString("\tif in == nil {\n\t\treturn nil\n\t}\n")
	b.WriteString("\tswitch kind := in.GetKind().(type) {\n")
	for _, field := range message.Oneof.Variants {
		variant := message.PublicName + field.ProtoGoName
		wrapper := message.ProtoName + "_" + field.ProtoGoName
		b.WriteString(fmt.Sprintf("\tcase *proto.%s:\n", wrapper))
		if field.Kind == irKindMessage {
			b.WriteString(fmt.Sprintf("\t\tvalue := %sFromProto(kind.%s)\n", publicMessageName(field.MessageName), field.ProtoGoName))
			b.WriteString(fmt.Sprintf("\t\tif value == nil {\n\t\t\treturn %s{}\n\t\t}\n", variant))
			b.WriteString(fmt.Sprintf("\t\treturn %s{%s: *value}\n", variant, field.ProtoGoName))
		} else {
			b.WriteString(fmt.Sprintf("\t\treturn %s{%s: kind.%s}\n", variant, field.ProtoGoName, field.ProtoGoName))
		}
	}
	b.WriteString(fmt.Sprintf("\tdefault:\n\t\treturn %sUnset{}\n\t}\n}\n\n", message.PublicName))

	b.WriteString(fmt.Sprintf("func proto%s(in %s) (*proto.%s, error) {\n", message.PublicName, message.PublicName, message.ProtoName))
	b.WriteString("\tif in == nil {\n\t\treturn nil, nil\n\t}\n")
	b.WriteString("\tswitch value := in.(type) {\n")
	for _, field := range message.Oneof.Variants {
		variant := message.PublicName + field.ProtoGoName
		wrapper := message.ProtoName + "_" + field.ProtoGoName
		b.WriteString(fmt.Sprintf("\tcase %s:\n", variant))
		if field.Kind == irKindMessage {
			b.WriteString(fmt.Sprintf("\t\twire, err := %sToProto(&value.%s)\n", publicMessageName(field.MessageName), field.ProtoGoName))
			b.WriteString("\t\tif err != nil {\n\t\t\treturn nil, err\n\t\t}\n")
			b.WriteString(fmt.Sprintf("\t\treturn &proto.%s{Kind: &proto.%s{%s: wire}}, nil\n", message.ProtoName, wrapper, field.ProtoGoName))
		} else {
			b.WriteString(fmt.Sprintf("\t\treturn &proto.%s{Kind: &proto.%s{%s: value.%s}}, nil\n", message.ProtoName, wrapper, field.ProtoGoName, field.ProtoGoName))
		}
	}
	b.WriteString(fmt.Sprintf("\tcase %sUnset:\n\t\treturn &proto.%s{}, nil\n", message.PublicName, message.ProtoName))
	b.WriteString(fmt.Sprintf("\tdefault:\n\t\treturn nil, fmt.Errorf(\"unsupported %s %%T\", in)\n\t}\n}\n\n", strings.ToLower(message.PublicName)))
}

func goFromProtoFieldExpr(ir authorizationIR, field irField, receiver string) string {
	getter := fmt.Sprintf("%s.Get%s()", receiver, field.ProtoGoName)
	if field.Repeated {
		return goFromProtoRepeatedExpr(ir, field, getter)
	}
	switch field.Kind {
	case irKindString, irKindBool, irKindInt32:
		return getter
	case irKindEnum:
		return fmt.Sprintf("%s(%s)", field.EnumName, getter)
	case irKindJSON:
		return fmt.Sprintf("mapFromStruct(%s)", getter)
	case irKindTimestamp:
		return fmt.Sprintf("timeFromProto(%s)", getter)
	case irKindMessage:
		if isGoOneofMessage(ir, field.MessageName) {
			return fmt.Sprintf("%sFromProto(%s)", lowerFirst(publicMessageName(field.MessageName)), getter)
		}
		return fmt.Sprintf("%sFromProto(%s)", publicMessageName(field.MessageName), getter)
	default:
		return getter
	}
}

func goFromProtoRepeatedExpr(ir authorizationIR, field irField, getter string) string {
	switch field.Kind {
	case irKindMessage:
		name := "slice" + publicMessageName(field.MessageName) + "FromProto"
		return fmt.Sprintf("%s(%s)", name, getter)
	default:
		return fmt.Sprintf("append([]%s(nil), %s...)", goBaseType(ir, field), getter)
	}
}

func fieldNeedsGoLocal(field irField) bool {
	return field.Kind == irKindJSON || field.Kind == irKindTimestamp || field.Kind == irKindMessage
}

func goToProtoLocal(ir authorizationIR, field irField, expr, label string) string {
	name := field.JSONName
	if field.Repeated {
		return fmt.Sprintf("\t%s, err := protoSlice%s(%s)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"%s: %%w\", err)\n\t}\n", name, publicMessageName(field.MessageName), expr, label)
	}
	switch field.Kind {
	case irKindJSON:
		return fmt.Sprintf("\t%s, err := structFromMap(%s)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"%s: %%w\", err)\n\t}\n", name, expr, label)
	case irKindTimestamp:
		return fmt.Sprintf("\t%s := timestampFromTime(%s)\n", name, expr)
	case irKindMessage:
		if isGoOneofMessage(ir, field.MessageName) {
			return fmt.Sprintf("\t%s, err := proto%s(%s)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"%s: %%w\", err)\n\t}\n", name, publicMessageName(field.MessageName), expr, label)
		}
		return fmt.Sprintf("\t%s, err := %sToProto(%s)\n\tif err != nil {\n\t\treturn nil, fmt.Errorf(\"%s: %%w\", err)\n\t}\n", name, publicMessageName(field.MessageName), expr, label)
	default:
		return ""
	}
}

func goToProtoFieldExpr(ir authorizationIR, field irField, expr, label string) string {
	if fieldNeedsGoLocal(field) {
		return field.JSONName
	}
	switch field.Kind {
	case irKindEnum:
		return fmt.Sprintf("proto.%s(%s)", field.EnumName, expr)
	default:
		return expr
	}
}

func renderGoSliceAndTimeHelpers(b *strings.Builder, ir authorizationIR) {
	b.WriteString(`func timeFromProto(in *timestamppb.Timestamp) *time.Time {
	if in == nil {
		return nil
	}
	value := in.AsTime()
	return &value
}

func timestampFromTime(in *time.Time) *timestamppb.Timestamp {
	if in == nil {
		return nil
	}
	return timestamppb.New(*in)
}

`)
	for _, message := range ir.Messages {
		if message.Empty {
			continue
		}
		var nativeType string
		if message.Oneof != nil {
			nativeType = message.PublicName
		} else {
			nativeType = "*" + message.PublicName
		}
		b.WriteString(fmt.Sprintf("func slice%sFromProto(in []*proto.%s) []%s {\n", message.PublicName, message.ProtoName, nativeType))
		b.WriteString(fmt.Sprintf("\tout := make([]%s, 0, len(in))\n", nativeType))
		b.WriteString("\tfor _, item := range in {\n")
		if message.Oneof != nil {
			b.WriteString(fmt.Sprintf("\t\tout = append(out, %sFromProto(item))\n", lowerFirst(message.PublicName)))
		} else {
			b.WriteString(fmt.Sprintf("\t\tout = append(out, %sFromProto(item))\n", message.PublicName))
		}
		b.WriteString("\t}\n\treturn out\n}\n\n")
		b.WriteString(fmt.Sprintf("func protoSlice%s(in []%s) ([]*proto.%s, error) {\n", message.PublicName, nativeType, message.ProtoName))
		b.WriteString(fmt.Sprintf("\tout := make([]*proto.%s, 0, len(in))\n", message.ProtoName))
		b.WriteString("\tfor i, item := range in {\n")
		if message.Oneof != nil {
			b.WriteString(fmt.Sprintf("\t\twire, err := proto%s(item)\n", message.PublicName))
		} else {
			b.WriteString(fmt.Sprintf("\t\twire, err := %sToProto(item)\n", message.PublicName))
		}
		b.WriteString("\t\tif err != nil {\n\t\t\treturn nil, fmt.Errorf(\"[%d]: %w\", i, err)\n\t\t}\n")
		b.WriteString("\t\tout = append(out, wire)\n\t}\n\treturn out, nil\n}\n\n")
	}
	b.WriteString(`func modelAllowedTargetsFromProto(in []*proto.ModelAllowedTarget) []ModelAllowedTarget {
	return sliceModelAllowedTargetFromProto(in)
}

func protoModelAllowedTargets(in []ModelAllowedTarget) ([]*proto.ModelAllowedTarget, error) {
	return protoSliceModelAllowedTarget(in)
}
`)
}
