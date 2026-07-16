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
	TargetTS           Target = "ts"
	TargetPython       Target = "python"
	TargetGo           Target = "go"
	TargetRust         Target = "rust"
	TargetPublicTS     Target = "public-ts"
	TargetPublicTSWeb  Target = "public-ts-web"
	TargetPublicPython Target = "public-python"
	TargetPublicGo     Target = "public-go"
	TargetPublicRust   Target = "public-rust"
)

// AllTargets returns every provider target in canonical order.
func AllTargets() []Target {
	return []Target{TargetTS, TargetPython, TargetGo, TargetRust}
}

// baseTarget returns the provider target that owns companion, or "" when t is
// already a provider target.
func baseTarget(t Target) Target {
	switch t {
	case TargetPublicTS, TargetPublicTSWeb:
		return TargetTS
	case TargetPublicPython:
		return TargetPython
	case TargetPublicGo:
		return TargetGo
	case TargetPublicRust:
		return TargetRust
	default:
		return t
	}
}

// IncludesTarget reports whether targets selects t, including when a companion
// public target is selected via its provider target.
func IncludesTarget(targets []Target, t Target) bool {
	want := baseTarget(t)
	for _, selected := range targets {
		if baseTarget(selected) == want {
			return true
		}
	}
	return false
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

	// StaleScope limits which generated files under OutputRoot are eligible for
	// stale deletion during reconcile. nil means the entire output tree is owned.
	StaleScope() func(rel string) bool
}
