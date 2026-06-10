// Package rust is the Rust SDK emitter. It emits nothing yet: this slice
// proves the pipeline; the Rust surface lands in a follow-up.
package rust

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetRust }

func (*Emitter) OutputRoot() string { return "sdk/rust/src" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Emit(*model.Schema) (*fileset.FileSet, error) { return fileset.New(), nil }

func (*Emitter) Formatter() *toolchain.Tool { return nil }
