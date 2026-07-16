// Package publicrust emits the public gestaltd Rust client under
// sdk/rust/src/public/generated/.
package publicrust

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/rust"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetPublicRust }

func (*Emitter) OutputRoot() string { return "sdk/rust/src/public" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Rustfmt() }

func (*Emitter) StaleScope() func(rel string) bool { return nil }

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	return rust.EmitPublic(schema)
}
