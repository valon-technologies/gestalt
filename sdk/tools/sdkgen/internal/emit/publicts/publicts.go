// Package publicts emits the browser- and Node-compatible public Gestalt
// transport client under sdk/typescript/src/client/generated/.
package publicts

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// OutputRoot is the repo-relative directory for generated public client files.
const OutputRoot = "sdk/typescript/src/client/generated"

// Emitter renders the public TypeScript transport client.
type Emitter struct{}

func New() *Emitter { return &Emitter{} }

func (*Emitter) HeaderStyle() fileset.CommentStyle { return fileset.Slash }

func (*Emitter) Formatter() *toolchain.Tool { return toolchain.Prettier() }

func (*Emitter) Emit(schema *model.Schema) (*fileset.FileSet, error) {
	view := publicsurface.Build(schema)
	idx := buildIndex(schema)
	set := fileset.New()

	files := map[string]string{
		"methods.ts":     renderMethods(view),
		"types.ts":       renderTypes(view, idx),
		"rest_clients.ts": renderClients(view, idx, true),
		"grpc_clients.ts": renderClients(view, idx, false),
	}
	for path, content := range files {
		if err := set.Add(path, []byte(content)); err != nil {
			return nil, err
		}
	}
	return set, nil
}

type index struct {
	messages map[string]*model.Message
	enums    map[string]*model.Enum
}

func buildIndex(schema *model.Schema) *index {
	idx := &index{
		messages: map[string]*model.Message{},
		enums:    map[string]*model.Enum{},
	}
	for _, m := range schema.Messages {
		idx.messages[m.FullName] = m
	}
	for _, e := range schema.Enums {
		idx.enums[e.FullName] = e
	}
	return idx
}
