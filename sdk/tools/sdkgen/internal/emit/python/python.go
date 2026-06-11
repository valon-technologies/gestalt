// Package python is the Python SDK emitter. It emits nothing yet: this slice
// proves the pipeline; the Python surface lands in a follow-up.
package python

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetPython }

func (*Emitter) OutputRoot() string { return "sdk/python/gestalt" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Hash }

func (*Emitter) Emit(*model.Schema) (*fileset.FileSet, error) { return fileset.New(), nil }

func (*Emitter) Formatter() *toolchain.Tool { return nil }
