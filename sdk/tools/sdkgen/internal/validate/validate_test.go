package validate

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func compileFixture(t *testing.T, filename string) protoreflect.FileDescriptor {
	t.Helper()
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{"testdata"},
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := compiler.Compile(context.Background(), filename)
	if err != nil {
		t.Fatalf("compile %s: %v", filename, err)
	}
	return files[0]
}

func fixtureServices(t *testing.T, filename string) []protoreflect.ServiceDescriptor {
	t.Helper()
	fd := compileFixture(t, filename)
	var out []protoreflect.ServiceDescriptor
	services := fd.Services()
	for i := 0; i < services.Len(); i++ {
		out = append(out, services.Get(i))
	}
	return out
}

func findMessage(t *testing.T, schema *model.Schema, fullName string) *model.Message {
	t.Helper()
	for _, m := range schema.Messages {
		if m.FullName == fullName {
			return m
		}
	}
	t.Fatalf("message %s not in schema", fullName)
	return nil
}

func findField(t *testing.T, m *model.Message, name string) *model.Field {
	t.Helper()
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %s not in message %s", name, m.FullName)
	return nil
}

func TestBuildClassifiesAllowedConstructs(t *testing.T) {
	t.Parallel()
	schema, diags := Build(fixtureServices(t, "widget.proto"), "")
	if !diags.Empty() {
		t.Fatalf("unexpected diagnostics:\n%v", diags.Err())
	}

	widget := findMessage(t, schema, "sdkgen.test.v1.Widget")

	cases := []struct {
		field    string
		kind     model.SemanticKind
		presence model.Presence
	}{
		{"count", model.KindScalar, model.NoPresence},
		{"nickname", model.KindScalar, model.ExplicitPresence},
		{"payload", model.KindBytes, model.NoPresence},
		{"gadget", model.KindMessage, model.ExplicitPresence},
		{"tags", model.KindRepeated, model.NoPresence},
		{"gadgets", model.KindRepeated, model.NoPresence},
		{"labels", model.KindMap, model.NoPresence},
		{"parts", model.KindMap, model.NoPresence},
		{"color", model.KindEnum, model.NoPresence},
		{"config", model.KindJSONStruct, model.ExplicitPresence},
		{"extra", model.KindJSONValue, model.ExplicitPresence},
		{"built_at", model.KindTimestamp, model.ExplicitPresence},
		{"child", model.KindMessage, model.ExplicitPresence},
		{"nothing", model.KindJSONNull, model.ExplicitPresence},
		{"unit", model.KindUnit, model.ExplicitPresence},
		{"ttl", model.KindDuration, model.ExplicitPresence},
		{"last_error", model.KindRPCStatus, model.ExplicitPresence},
	}
	for _, tc := range cases {
		f := findField(t, widget, tc.field)
		if f.Kind != tc.kind {
			t.Errorf("field %s: kind = %v, want %v", tc.field, f.Kind, tc.kind)
		}
		if f.Presence != tc.presence {
			t.Errorf("field %s: presence = %v, want %v", tc.field, f.Presence, tc.presence)
		}
	}

	if f := findField(t, widget, "count"); f.Scalar != model.ScalarInt64 {
		t.Errorf("count scalar = %v, want int64", f.Scalar)
	}
	if f := findField(t, widget, "tags"); f.Elem == nil || f.Elem.Kind != model.KindScalar || f.Elem.Scalar != model.ScalarString {
		t.Errorf("tags elem = %+v, want scalar string", f.Elem)
	}
	if f := findField(t, widget, "gadgets"); f.Elem == nil || f.Elem.Kind != model.KindMessage || f.Elem.Message != "sdkgen.test.v1.Gadget" {
		t.Errorf("gadgets elem = %+v, want message Gadget", f.Elem)
	}
	if f := findField(t, widget, "labels"); f.MapKey != model.ScalarString || f.MapValue == nil || f.MapValue.Kind != model.KindScalar {
		t.Errorf("labels map = key %v value %+v, want string->string", f.MapKey, f.MapValue)
	}
	if f := findField(t, widget, "parts"); f.MapValue == nil || f.MapValue.Kind != model.KindMessage || f.MapValue.Message != "sdkgen.test.v1.Gadget" {
		t.Errorf("parts map value = %+v, want message Gadget", f.MapValue)
	}
	if f := findField(t, widget, "color"); f.Enum != "sdkgen.test.v1.WidgetColor" {
		t.Errorf("color enum = %q, want WidgetColor", f.Enum)
	}

	// The real oneof is modeled; the synthetic oneof backing the optional
	// scalar is presence, not a variant group.
	if len(widget.Oneofs) != 1 {
		t.Fatalf("oneofs = %d, want 1", len(widget.Oneofs))
	}
	oneof := widget.Oneofs[0]
	if oneof.Name != "contents" {
		t.Errorf("oneof name = %q, want contents", oneof.Name)
	}
	if !reflect.DeepEqual(oneof.FieldNumbers, []int32{14, 15, 16, 17}) {
		t.Errorf("oneof field numbers = %v, want [14 15 16 17]", oneof.FieldNumbers)
	}
	if f := findField(t, widget, "text"); f.OneofIndex != 0 {
		t.Errorf("text oneof index = %d, want 0", f.OneofIndex)
	}
	if f := findField(t, widget, "nickname"); f.OneofIndex != -1 {
		t.Errorf("nickname oneof index = %d, want -1 (synthetic)", f.OneofIndex)
	}

	if len(schema.Enums) != 1 || schema.Enums[0].FullName != "sdkgen.test.v1.WidgetColor" {
		t.Errorf("enums = %+v, want exactly WidgetColor", schema.Enums)
	}
	if got := schema.Enums[0].Values; len(got) != 2 || got[1].Name != "WIDGET_COLOR_RED" || got[1].Number != 1 {
		t.Errorf("enum values = %+v", got)
	}
}

func TestBuildClassifiesMethods(t *testing.T) {
	t.Parallel()
	schema, diags := Build(fixtureServices(t, "widget.proto"), "")
	if !diags.Empty() {
		t.Fatalf("unexpected diagnostics:\n%v", diags.Err())
	}
	if len(schema.Services) != 1 {
		t.Fatalf("services = %d, want 1", len(schema.Services))
	}
	methods := map[string]*model.Method{}
	for _, m := range schema.Services[0].Methods {
		methods[m.Name] = m
	}

	cases := []struct {
		name   string
		stream model.StreamKind
	}{
		{"GetWidget", model.Unary},
		{"Ping", model.Unary},
		{"ReadChunks", model.ServerStream},
		{"WriteChunks", model.ClientStream},
		{"Exchange", model.Bidi},
	}
	for _, tc := range cases {
		m, ok := methods[tc.name]
		if !ok {
			t.Fatalf("method %s missing", tc.name)
		}
		if m.Stream != tc.stream {
			t.Errorf("method %s: stream = %v, want %v", tc.name, m.Stream, tc.stream)
		}
	}

	ping := methods["Ping"]
	if !ping.InputIsEmpty || !ping.OutputIsEmpty || ping.Input != nil || ping.Output != nil {
		t.Errorf("Ping empty flags = %+v", ping)
	}
	get := methods["GetWidget"]
	if get.InputIsEmpty || get.Input == nil || get.Input.FullName != "sdkgen.test.v1.WidgetRequest" {
		t.Errorf("GetWidget input = %+v", get.Input)
	}
}

func TestBuildRejectsUnsupportedConstructs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file       string
		wantDetail string
	}{
		{"rejected_any.proto", "unsupported well-known type google.protobuf.Any"},
		{"rejected_fieldmask.proto", "unsupported well-known type google.protobuf.FieldMask"},
		{"rejected_proto2.proto", "unsupported syntax proto2"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			_, diags := Build(fixtureServices(t, tc.file), "")
			all := diags.All()
			if len(all) != 1 {
				t.Fatalf("diagnostics = %d, want 1:\n%v", len(all), diags.Err())
			}
			d := all[0]
			if d.ProtoFile != tc.file {
				t.Errorf("diagnostic file = %q, want %q", d.ProtoFile, tc.file)
			}
			if d.Line <= 0 {
				t.Errorf("diagnostic line = %d, want > 0", d.Line)
			}
			if !strings.Contains(d.Detail, tc.wantDetail) {
				t.Errorf("diagnostic detail = %q, want substring %q", d.Detail, tc.wantDetail)
			}
		})
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	t.Parallel()
	services := fixtureServices(t, "widget.proto")
	first, diags := Build(services, "")
	if !diags.Empty() {
		t.Fatalf("unexpected diagnostics:\n%v", diags.Err())
	}
	second, _ := Build(services, "")
	if !reflect.DeepEqual(first, second) {
		t.Error("two builds of the same descriptor differ")
	}
}
