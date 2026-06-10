// Package emit defines the contract between the normalized model and the
// per-language emitters.
package emit

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// Target identifies a language emitter.
type Target string

const (
	TargetTS     Target = "ts"
	TargetPython Target = "python"
	TargetGo     Target = "go"
	TargetRust   Target = "rust"
)

// AllTargets returns every target in canonical order.
func AllTargets() []Target {
	return []Target{TargetTS, TargetPython, TargetGo, TargetRust}
}

// ParseTargets parses a comma-separated target list, preserving canonical
// order and rejecting unknown or duplicate entries.
func ParseTargets(s string) ([]Target, error) {
	if strings.TrimSpace(s) == "" {
		return AllTargets(), nil
	}
	seen := map[Target]bool{}
	for _, part := range strings.Split(s, ",") {
		t := Target(strings.TrimSpace(part))
		switch t {
		case TargetTS, TargetPython, TargetGo, TargetRust:
		default:
			return nil, fmt.Errorf("unknown target %q (valid: ts, python, go, rust)", part)
		}
		if seen[t] {
			return nil, fmt.Errorf("duplicate target %q", t)
		}
		seen[t] = true
	}
	var out []Target
	for _, t := range AllTargets() {
		if seen[t] {
			out = append(out, t)
		}
	}
	return out, nil
}

// Emitter renders one language's SDK surface from the normalized model. Each
// emitter owns the generated files under OutputRoot per file (via the
// generated-by header), not per directory: generated code is the public SDK
// surface and lives alongside handwritten files.
type Emitter interface {
	Target() Target

	// OutputRoot is the repo-relative directory whose sdkgen-owned files this
	// emitter reconciles.
	OutputRoot() string

	HeaderStyle() fileset.CommentStyle

	// Emit renders the complete file set for the schema. Paths are relative
	// to OutputRoot and exclude the generated-by header.
	Emit(schema *model.Schema) (*fileset.FileSet, error)

	// Formatter is the pinned formatter run over emitted files, or nil while
	// the emitter produces no output.
	Formatter() *toolchain.Tool
}
