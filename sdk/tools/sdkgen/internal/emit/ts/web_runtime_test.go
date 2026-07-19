package ts

import (
	"strings"
	"testing"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func TestRenderWebNativeTypesEmitsSupportTypeImports(t *testing.T) {
	t.Parallel()

	idx := &index{
		messages: map[string]*model.Message{},
		enums:    map[string]*model.Enum{},
	}
	msg := &model.Message{
		FullName:  "gestalt.provider.v1.Widget",
		Name:      "Widget",
		ProtoFile: "sdk/proto/v1/widget.proto",
		Fields: []*model.Field{
			{Name: "timeout", JSONName: "timeout", Kind: model.KindDuration, OneofIndex: -1},
			{Name: "result", JSONName: "result", Kind: model.KindUnit, OneofIndex: -1},
			{Name: "status", JSONName: "status", Kind: model.KindRPCStatus, OneofIndex: -1},
			{Name: "object", JSONName: "object", Kind: model.KindJSONStruct, OneofIndex: -1},
			{Name: "value", JSONName: "value", Kind: model.KindJSONValue, OneofIndex: -1},
		},
	}
	idx.messages[msg.FullName] = msg

	imports := PublicImports{SupportPrefix: ".", FixedNativeModule: "native-types.ts"}
	out := renderWebNativeTypes(idx, []*model.Message{msg}, nil, imports)

	for _, want := range []string{
		`import type { DurationMs, JsonInput, JsonObjectInput, RpcStatus, Unit } from "./rpc_support.ts"`,
		"timeout: DurationMs",
		"result: Unit",
		"status: RpcStatus",
		"object: JsonObjectInput",
		"value: JsonInput",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in native-types:\n%s", want, out)
		}
	}
}
