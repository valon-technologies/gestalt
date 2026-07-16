// Package publicpython emits the public gestaltd Python client under
// sdk/python/gestalt/public/generated/.
package publicpython

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/python"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetPublicPython }

func (*Emitter) OutputRoot() string { return "sdk/python/gestalt/public" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Hash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Ruff() }

func (*Emitter) StaleScope() func(rel string) bool { return nil }

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	return python.EmitPublic(schema)
}
