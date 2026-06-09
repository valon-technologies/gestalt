//nolint:gocritic // Emitters assemble target-language source strings where Sprintf keeps fragments readable.
package main

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
)

func renderGoProviderSDK(ir ProviderSDKIR) ([]generatedFile, error) {
	packagePath := "sdk/go/" + ir.Config.Package["go"]
	outputs := []string{
		packagePath + "/doc.go",
		packagePath + "/types.go",
		packagePath + "/protocol.go",
		packagePath + "/conversions.go",
		packagePath + "/client.go",
	}
	files := make([]generatedFile, 0, len(outputs))
	for _, path := range outputs {
		var body string
		switch {
		case strings.HasSuffix(path, "/doc.go"):
			body = renderGoDoc(ir)
		case strings.HasSuffix(path, "/types.go"):
			body = renderGoTypes(ir)
		case strings.HasSuffix(path, "/protocol.go"):
			body = renderGoProtocol(ir)
		case strings.HasSuffix(path, "/conversions.go"):
			body = renderGoConversions(ir)
		case strings.HasSuffix(path, "/client.go"):
			body = renderGoClient(ir)
		default:
			return nil, fmt.Errorf("%s: unsupported go output %s", ir.Config.Proto, path)
		}
		data := append(generatedHeader(path), []byte(body)...)
		formatted, err := format.Source(data)
		if err != nil {
			return nil, fmt.Errorf("format %s: %w", path, err)
		}
		files = append(files, generatedFile{Path: path, Data: formatted})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func renderGoDoc(ir ProviderSDKIR) string {
	return fmt.Sprintf(`// Package %s contains the canonical Gestalt %s SDK model
// and gRPC host-service client.
package %s
`, ir.Config.Package["go"], ir.Config.SDKName, ir.Config.Package["go"])
}

func renderGoTypes(ir ProviderSDKIR) string {
	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", ir.Config.Package["go"])
	b.WriteString("import \"time\"\n\n")
	for i := range ir.Messages {
		message := ir.Messages[i]
		if message.Empty {
			continue
		}
		if message.Oneof != nil {
			renderGoOneofType(&b, ir, message)
			continue
		}
		fmt.Fprintf(&b, "type %s struct {\n", message.PublicName)
		for j := range message.Fields {
			field := message.Fields[j]
			fmt.Fprintf(&b, "\t%s %s\n", field.GoName, goType(ir, field))
		}
		b.WriteString("}\n\n")
	}
	for i := range ir.Enums {
		enum := ir.Enums[i]
		fmt.Fprintf(&b, "type %s int32\n\n", enum.ProtoName)
		b.WriteString("const (\n")
		for j := range enum.Values {
			value := enum.Values[j]
			fmt.Fprintf(&b, "\t%s %s = %d\n", value.GoName, enum.ProtoName, value.Number)
		}
		b.WriteString(")\n\n")
	}
	return b.String()
}

func renderGoOneofType(b *strings.Builder, ir ProviderSDKIR, message irMessage) {
	fmt.Fprintf(b, "type %s interface {\n\tis%s()\n}\n\n", message.PublicName, message.PublicName)
	for i := range message.Oneof.Variants {
		field := message.Oneof.Variants[i]
		variant := message.PublicName + goFieldName(field.ProtoName)
		fmt.Fprintf(b, "type %s struct {\n\t%s %s\n}\n\n", variant, goFieldName(field.ProtoName), goOneofVariantType(ir, field))
	}
	fmt.Fprintf(b, "type %sUnset struct{}\n\n", message.PublicName)
	for i := range message.Oneof.Variants {
		field := message.Oneof.Variants[i]
		variant := message.PublicName + goFieldName(field.ProtoName)
		fmt.Fprintf(b, "func (%s) is%s() {}\n", variant, message.PublicName)
	}
	fmt.Fprintf(b, "func (%sUnset) is%s() {}\n\n", message.PublicName, message.PublicName)
	for i := range message.Oneof.Variants {
		field := message.Oneof.Variants[i]
		variant := message.PublicName + goFieldName(field.ProtoName)
		name := goConstructorName(message, field)
		fmt.Fprintf(b, "func %s(value %s) %s {\n\treturn %s{%s: value}\n}\n\n", name, goOneofVariantType(ir, field), message.PublicName, variant, goFieldName(field.ProtoName))
	}
	fmt.Fprintf(b, "func Unset%s() %s {\n\treturn %sUnset{}\n}\n\n", message.PublicName, message.PublicName, message.PublicName)
}

func renderGoProtocol(ir ProviderSDKIR) string {
	return "package " + ir.Config.Package["go"] + `

import (
	"encoding/json"
	"fmt"
	"math"

	"google.golang.org/protobuf/types/known/structpb"
)

func structFromMap(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		value = map[string]any{}
	}
	normalized, err := normalizeJSON(value, "struct")
	if err != nil {
		return nil, err
	}
	object, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("struct must be a JSON object")
	}
	return structpb.NewStruct(object)
}

func mapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func normalizeJSON(value any, path string) (any, error) {
	switch v := value.(type) {
	case nil, bool, string:
		return v, nil
	case int:
		return v, nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case float32:
		f := float64(v)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%s must be a finite number", path)
		}
		return f, nil
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, fmt.Errorf("%s must be a finite number", path)
		}
		return v, nil
	case []any:
		out := make([]any, 0, len(v))
		for i, item := range v {
			normalized, err := normalizeJSON(item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			normalized, err := normalizeJSON(item, path+"."+key)
			if err != nil {
				return nil, err
			}
			out[key] = normalized
		}
		return out, nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		f, err := v.Float64()
		if err != nil {
			return nil, fmt.Errorf("%s must be a JSON number: %w", path, err)
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%s must be a finite number", path)
		}
		return f, nil
	default:
		return nil, fmt.Errorf("%s must be JSON-compatible, got %T", path, value)
	}
}
`
}

//nolint:staticcheck // This emitter intentionally assembles generated Go source fragments.
func renderGoClient(ir ProviderSDKIR) string {
	pkg := ir.Config.Package["go"]
	serviceKind := strings.ToLower(ir.Config.SDKName)
	clientType := ir.ServiceName + "Client"
	newClient := "New" + ir.ServiceName + "Client"
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`package %s

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/sdk/go/internal/host"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type Client struct {
	conn   *grpc.ClientConn
	client proto.%s
}

type Option func(*options)

type options struct {
	target string
}

func WithTarget(target string) Option {
	return func(opts *options) {
		opts.target = target
	}
}

func New(ctx context.Context, opts ...Option) (*Client, error) {
	cfg := options{}
	for _, opt := range opts {
		opt(&cfg)
	}

	target := cfg.target
	token := ""
	var err error
	if target == "" {
		target, token, err = host.Target(%q)
		if err != nil {
			return nil, err
		}
	} else {
		token = strings.TrimSpace(os.Getenv(host.EnvHostServiceToken))
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := host.DialService(dialCtx, %[3]q, target, token, "")
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, client: proto.%[4]s(conn)}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	conn := c.conn
	c.conn = nil
	c.client = nil
	return conn.Close()
}

`, pkg, clientType, serviceKind, newClient))
	for _, method := range ir.Methods {
		output := ir.MessagesByName[method.OutputName]
		if method.EmptyInput {
			b.WriteString(fmt.Sprintf("func (c *Client) %s(ctx context.Context) (*%s, error) {\n", method.ProtoName, output.PublicName))
			b.WriteString(fmt.Sprintf("\tif c == nil || c.client == nil {\n\t\treturn nil, fmt.Errorf(%q)\n\t}\n", serviceKind+": client is not initialized"))
			b.WriteString(fmt.Sprintf("\tresp, err := c.client.%s(ctx, &emptypb.Empty{})\n", method.ProtoName))
			b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
			b.WriteString(fmt.Sprintf("\treturn %sFromProto(resp), nil\n}\n\n", output.PublicName))
			continue
		}
		input := ir.MessagesByName[method.InputName]
		b.WriteString(fmt.Sprintf("func (c *Client) %s(ctx context.Context, req %s) (*%s, error) {\n", method.ProtoName, input.PublicName, output.PublicName))
		b.WriteString(fmt.Sprintf("\tif c == nil || c.client == nil {\n\t\treturn nil, fmt.Errorf(%q)\n\t}\n", serviceKind+": client is not initialized"))
		b.WriteString(fmt.Sprintf("\twire, err := %sToProto(&req)\n", input.PublicName))
		b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		b.WriteString(fmt.Sprintf("\tresp, err := c.client.%s(ctx, wire)\n", method.ProtoName))
		b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		b.WriteString(fmt.Sprintf("\treturn %sFromProto(resp), nil\n}\n\n", output.PublicName))
	}
	return b.String()
}

func goType(ir ProviderSDKIR, field irField) string {
	base := goBaseType(ir, field)
	if field.Repeated {
		if field.Kind == irKindMessage && !isGoOneofMessage(ir, field.MessageName) {
			return "[]*" + base
		}
		return "[]" + base
	}
	if field.Kind == irKindMessage && !isGoOneofMessage(ir, field.MessageName) {
		return "*" + base
	}
	if field.Kind == irKindTimestamp {
		return "*time.Time"
	}
	return base
}

func goBaseType(ir ProviderSDKIR, field irField) string {
	switch field.Kind {
	case irKindString:
		return "string"
	case irKindBool:
		return "bool"
	case irKindInt32:
		return "int32"
	case irKindEnum:
		return field.EnumName
	case irKindJSON:
		return "map[string]any"
	case irKindTimestamp:
		return "time.Time"
	case irKindMessage:
		return ir.MessagesByName[field.MessageName].PublicName
	default:
		return "any"
	}
}

func goOneofVariantType(ir ProviderSDKIR, field irField) string {
	switch field.Kind {
	case irKindString:
		return "string"
	case irKindMessage:
		return publicMessageName(ir.Config, field.MessageName)
	default:
		return "any"
	}
}

func isGoOneofMessage(ir ProviderSDKIR, name string) bool {
	message, ok := ir.MessagesByName[name]
	return ok && message.Oneof != nil
}

func goConstructorName(message irMessage, field irField) string {
	if message.ProtoName == "RelationshipTarget" {
		switch field.ProtoName {
		case "subject":
			return "SubjectTarget"
		case "resource":
			return "ResourceTarget"
		case "subject_set":
			return "SubjectSetTarget"
		}
	}
	if message.ProtoName == "ModelAllowedTarget" {
		switch field.ProtoName {
		case "subject_type":
			return "SubjectTypeTarget"
		case "resource_type":
			return "ResourceTypeTarget"
		case "subject_set_type":
			return "SubjectSetTypeTarget"
		}
	}
	return goFieldName(field.ProtoName) + message.PublicName
}
