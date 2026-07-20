package publictsweb

import (
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/ts"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

const OutputRoot = "sdk/typescript-web/src/client"

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetPublicTSWeb }

func (*Emitter) OutputRoot() string { return OutputRoot }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Prettier() }

func (*Emitter) StaleScope() func(rel string) bool {
	return func(rel string) bool {
		return strings.HasPrefix(rel, "generated/") || strings.HasPrefix(rel, "runtime/")
	}
}

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	plan, err := publicsurface.PrepareRESTEmit(schema)
	if err != nil {
		return nil, err
	}
	imports := ts.WebPublicImports()
	gen, err := ts.EmitPublicPlan(plan, imports)
	if err != nil {
		return nil, err
	}
	set, err := gen.Prefix("generated")
	if err != nil {
		return nil, err
	}
	runtime, err := ts.EmitWebRuntime(plan)
	if err != nil {
		return nil, err
	}
	runtimePrefixed, err := runtime.Prefix("runtime")
	if err != nil {
		return nil, err
	}
	if err := set.Merge(runtimePrefixed); err != nil {
		return nil, err
	}
	return set, nil
}
