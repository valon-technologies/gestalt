package validate

import (
	"context"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/diag"
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

func fixtureServices(t *testing.T, filename string) (*protoregistry.Files, []protoreflect.ServiceDescriptor) {
	t.Helper()
	fd := compileFixture(t, filename)
	registry := &protoregistry.Files{}
	var register func(protoreflect.FileDescriptor)
	register = func(f protoreflect.FileDescriptor) {
		if _, err := registry.FindFileByPath(f.Path()); err == nil {
			return
		}
		imports := f.Imports()
		for i := 0; i < imports.Len(); i++ {
			register(imports.Get(i).FileDescriptor)
		}
		if err := registry.RegisterFile(f); err != nil {
			t.Fatal(err)
		}
	}
	register(fd)
	var out []protoreflect.ServiceDescriptor
	services := fd.Services()
	for i := 0; i < services.Len(); i++ {
		out = append(out, services.Get(i))
	}
	return registry, out
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
			_, diags := build(t, tc.file)
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

// build compiles a fixture and runs Build over its services.
func build(t *testing.T, filename string) (*model.Schema, *diag.List) {
	t.Helper()
	registry, services := fixtureServices(t, filename)
	return Build(registry, services, "")
}

func TestBuildRejectsBadJsonResult(t *testing.T) {
	t.Parallel()
	_, diags := build(t, "rejected_json_result.proto")
	all := diags.All()
	wants := []string{
		`json_result status "status" must be a plain int32 field`,
		`json_result body "body" must be a bytes field`,
		"json_result annotation requires a unary method",
		`optional_signature field "id" repeats the signature list`,
		`optional_signature names unknown request field "nope"`,
		"already carries a response collapse",
	}
	if len(all) != len(wants) {
		t.Fatalf("diagnostics = %d, want %d:\n%v", len(all), len(wants), diags.Err())
	}
	for i, want := range wants {
		if !strings.Contains(all[i].Detail, want) {
			t.Errorf("diagnostic %d = %q, want substring %q", i, all[i].Detail, want)
		}
	}
}

func TestBuildRejectsBadAnnotations(t *testing.T) {
	t.Parallel()
	_, diags := build(t, "rejected_annotations.proto")
	all := diags.All()
	if len(all) != 2 {
		t.Fatalf("diagnostics = %d, want 2:\n%v", len(all), diags.Err())
	}
	wants := []string{"optional_result guard", "signature names unknown request field"}
	for i, want := range wants {
		if !strings.Contains(all[i].Detail, want) {
			t.Errorf("diagnostic %d = %q, want substring %q", i, all[i].Detail, want)
		}
	}
}
