// Package publicgo is the Go public gestaltd client emitter. It renders
// native types, method metadata, UnaryTransport, and AppClient into
// sdk/go/publicclient.
package publicgo

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/golang"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) Target() emit.Target { return emit.TargetPublicGo }

func (*Emitter) OutputRoot() string { return "sdk/go/publicclient" }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Gofmt() }

func (*Emitter) StaleScope() func(rel string) bool { return nil }

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	return golang.EmitPublic(schema)
}
