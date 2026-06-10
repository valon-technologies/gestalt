// Package golang is the Go SDK emitter. It emits nothing yet: this slice
// proves the pipeline; the Go surface lands in a follow-up.
package golang

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetGo }

func (*Emitter) OutputRoot() string { return "sdk/go" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Emit(*model.Schema) (*fileset.FileSet, error) { return fileset.New(), nil }

func (*Emitter) Formatter() *toolchain.Tool { return nil }
