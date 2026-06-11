// Package pipeline orchestrates a full sdkgen run: verify toolchain, build
// and load the descriptor image, discover and validate services, regenerate
// wire stubs, and emit and reconcile (or check) the SDK surfaces.
package pipeline

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/descriptor"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/discover"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/golang"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/python"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/rust"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/ts"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/validate"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/wire"
)

// ProtoRel is the proto module, relative to the repo root.
const ProtoRel = "sdk/proto"

// ErrDiagnostics reports unsupported proto constructs; nothing was written.
var ErrDiagnostics = errors.New("unsupported proto constructs")

// ErrDrift reports that checked-in generated files do not match a fresh
// render.
var ErrDrift = errors.New("drift detected")

type Options struct {
	RepoRoot string
	Targets  []emit.Target
	Check    bool
	Stdout   io.Writer
	Stderr   io.Writer
}

// Run executes a plain or check run per Options.
func Run(opts Options) error {
	emitters := emittersFor(opts.Targets)

	bufTool := toolchain.Buf()
	if err := bufTool.Verify(); err != nil {
		return err
	}
	rustfmtTool := toolchain.Rustfmt()
	if slices.Contains(opts.Targets, emit.TargetRust) {
		if err := rustfmtTool.Verify(); err != nil {
			return err
		}
	}
	for _, e := range emitters {
		if f := e.Formatter(); f != nil {
			if err := f.Verify(); err != nil {
				return err
			}
		}
	}

	scratch, err := os.MkdirTemp("", "sdkgen-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	schema, err := buildSchema(bufTool, opts.RepoRoot, scratch, opts.Stderr)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(opts.Stdout, "sdkgen: validated %d services, %d methods\n", len(schema.Services), methodCount(schema))

	if opts.Check {
		return check(bufTool, rustfmtTool, opts, emitters, schema, scratch)
	}
	return generate(bufTool, rustfmtTool, opts, emitters, schema, scratch)
}

// BuildSchema compiles, discovers, and validates the real proto module,
// returning the normalized model. Exposed for the pipeline test.
func BuildSchema(bufTool *toolchain.Tool, repoRoot, scratch string) (*model.Schema, error) {
	return buildSchema(bufTool, repoRoot, scratch, io.Discard)
}

func buildSchema(bufTool *toolchain.Tool, repoRoot, scratch string, stderr io.Writer) (*model.Schema, error) {
	protoDir := filepath.Join(repoRoot, ProtoRel)
	imagePath := filepath.Join(scratch, "image.binpb")
	if err := descriptor.BuildImage(bufTool, protoDir, imagePath); err != nil {
		return nil, err
	}
	files, err := descriptor.Load(imagePath)
	if err != nil {
		return nil, err
	}
	services := discover.Services(files)
	schema, diags := validate.Build(services, ProtoRel+"/")
	if !diags.Empty() {
		for _, d := range diags.All() {
			_, _ = fmt.Fprintln(stderr, d)
		}
		return nil, fmt.Errorf("%w: %d diagnostics", ErrDiagnostics, len(diags.All()))
	}
	return schema, nil
}

func generate(bufTool, rustfmtTool *toolchain.Tool, opts Options, emitters []emit.Emitter, schema *model.Schema, scratch string) error {
	if err := wire.Generate(bufTool, rustfmtTool, opts.RepoRoot, scratch, opts.Targets); err != nil {
		return err
	}
	for _, e := range emitters {
		set, err := e.Emit(schema)
		if err != nil {
			return fmt.Errorf("emit %s: %w", e.Target(), err)
		}
		root := filepath.Join(opts.RepoRoot, filepath.FromSlash(e.OutputRoot()))
		report, err := fileset.Reconcile(root, set, e.HeaderStyle())
		if err != nil {
			return fmt.Errorf("reconcile %s: %w", e.Target(), err)
		}
		_, _ = fmt.Fprintf(opts.Stdout, "sdkgen: %s: %d files emitted, %d written, %d stale removed\n",
			e.Target(), set.Len(), len(report.Written), len(report.Deleted))
	}
	return nil
}

func check(bufTool, rustfmtTool *toolchain.Tool, opts Options, emitters []emit.Emitter, schema *model.Schema, scratch string) error {
	drift, err := wire.Check(bufTool, rustfmtTool, opts.RepoRoot, scratch, opts.Targets)
	if err != nil {
		return err
	}
	for _, e := range emitters {
		set, err := e.Emit(schema)
		if err != nil {
			return fmt.Errorf("emit %s: %w", e.Target(), err)
		}
		root := filepath.Join(opts.RepoRoot, filepath.FromSlash(e.OutputRoot()))
		d, err := fileset.Check(root, set, e.HeaderStyle())
		if err != nil {
			return fmt.Errorf("check %s: %w", e.Target(), err)
		}
		for _, entry := range d {
			drift = append(drift, fileset.DriftEntry{Kind: entry.Kind, Path: path.Join(e.OutputRoot(), entry.Path)})
		}
	}
	if len(drift) == 0 {
		_, _ = fmt.Fprintln(opts.Stdout, "sdkgen: no drift")
		return nil
	}
	for _, entry := range drift {
		_, _ = fmt.Fprintf(opts.Stderr, "%-9s %s\n", entry.Kind, entry.Path)
	}
	return fmt.Errorf("%w: %d files; run `sdkgen` to regenerate", ErrDrift, len(drift))
}

// Emitters returns every registered emitter in canonical target order.
func Emitters() []emit.Emitter {
	return []emit.Emitter{ts.New(), python.New(), golang.New(), rust.New()}
}

func emittersFor(targets []emit.Target) []emit.Emitter {
	var out []emit.Emitter
	for _, e := range Emitters() {
		if slices.Contains(targets, e.Target()) {
			out = append(out, e)
		}
	}
	return out
}

// FindRepoRoot walks up from the working directory to the directory holding
// go.work, the repo root.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("repo root not found (no go.work in any parent directory)")
		}
		dir = parent
	}
}

func methodCount(schema *model.Schema) int {
	n := 0
	for _, svc := range schema.Services {
		n += len(svc.Methods)
	}
	return n
}
