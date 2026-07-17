// Package publicts emits the Node-compatible public Gestalt transport client
// under sdk/typescript/src/client/generated/.
package publicts

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/ts"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// OutputRoot is the repo-relative directory for generated public client files.
const OutputRoot = "sdk/typescript/src/client/generated"

// Emitter renders the public TypeScript transport client for gestalt.
type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetPublicTS }

func (*Emitter) OutputRoot() string { return OutputRoot }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Prettier() }

func (*Emitter) StaleScope() func(rel string) bool { return nil }

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	return ts.EmitPublic(schema, ts.ServerPublicImports())
}
