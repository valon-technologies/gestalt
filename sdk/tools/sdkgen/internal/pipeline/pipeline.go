// Package pipeline orchestrates a full sdkgen run: verify toolchain, build
// and load the descriptor image, discover and validate services, regenerate
// wire stubs, and emit and reconcile (or check) the SDK surfaces.
package pipeline

import (
	"errors"
	"fmt"
	"os"
	"io"
	"path"
	"path/filepath"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/descriptor"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/discover"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/golang"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/publicgo"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/publicpython"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/publicrust"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/publicts"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/publictsweb"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit/publicrpc"
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
	if emit.IncludesTarget(opts.Targets, emit.TargetRust) {
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
	schema, diags := validate.Build(files, services, ProtoRel+"/")
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
		set, err := EmitFormatted(e, schema, scratch, opts.RepoRoot)
		if err != nil {
			return err
		}
		root := filepath.Join(opts.RepoRoot, filepath.FromSlash(e.OutputRoot()))
		report, err := fileset.Reconcile(root, set, e.HeaderStyle(), e.StaleScope())
		if err != nil {
			return fmt.Errorf("reconcile %s: %w", e.Target(), err)
		}
		_, _ = fmt.Fprintf(opts.Stdout, "sdkgen: %s: %d files emitted, %d written, %d stale removed\n",
			e.Target(), set.Len(), len(report.Written), len(report.Deleted))
	}
	return reconcileRESTGateway(opts, schema)
}

func reconcileRESTGateway(opts Options, schema *model.Schema) error {
	content, err := publicrpc.EmitRESTGateway(schema)
	if err != nil {
		return err
	}
	path := filepath.Join(opts.RepoRoot, "gestaltd/internal/publicrpc/rest_gateway_gen.go")
	if opts.Check {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("check publicrpc: %w", readErr)
		}
		if string(existing) != content {
			return fmt.Errorf("%w: gestaltd/internal/publicrpc/rest_gateway_gen.go", ErrDrift)
		}
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(opts.Stdout, "sdkgen: publicrpc: rest_gateway_gen.go written")
	return nil
}

func check(bufTool, rustfmtTool *toolchain.Tool, opts Options, emitters []emit.Emitter, schema *model.Schema, scratch string) error {
	drift, err := wire.Check(bufTool, rustfmtTool, opts.RepoRoot, scratch, opts.Targets)
	if err != nil {
		return err
	}
	for _, e := range emitters {
		set, err := EmitFormatted(e, schema, scratch, opts.RepoRoot)
		if err != nil {
			return err
		}
		root := filepath.Join(opts.RepoRoot, filepath.FromSlash(e.OutputRoot()))
		d, err := fileset.Check(root, set, e.HeaderStyle(), e.StaleScope())
		if err != nil {
			return fmt.Errorf("check %s: %w", e.Target(), err)
		}
		for _, entry := range d {
			drift = append(drift, fileset.DriftEntry{Kind: entry.Kind, Path: path.Join(e.OutputRoot(), entry.Path)})
		}
	}
	if err := reconcileRESTGateway(opts, schema); err != nil {
		if errors.Is(err, ErrDrift) {
			drift = append(drift, fileset.DriftEntry{Kind: fileset.Modified, Path: "gestaltd/internal/publicrpc/rest_gateway_gen.go"})
		} else {
			return err
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

// EmitFormatted renders an emitter's file set and runs its formatter over the
// contents via a scratch round-trip, so reconcile and check always see final
// bytes. The generated-by header is injected later, at write time; leading
// comments are stable under gofmt and rustfmt.
func EmitFormatted(e emit.Emitter, schema *model.Schema, scratch, repoRoot string) (*fileset.FileSet, error) {
	set, err := e.Emit(schema)
	if err != nil {
		return nil, fmt.Errorf("emit %s: %w", e.Target(), err)
	}
	tool := e.Formatter()
	if tool == nil || set.Len() == 0 {
		return set, nil
	}
	dir := filepath.Join(scratch, "format", string(e.Target()))
	args := append([]string{}, tool.FormatArgs...)
	for _, f := range set.Files() {
		abs := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, f.Content, 0o644); err != nil {
			return nil, err
		}
		args = append(args, abs)
	}
	if tool.Name == "ruff" && repoRoot != "" {
		configPath := filepath.Join(dir, "pyproject.toml")
		src := filepath.Join(repoRoot, "sdk", "python", "pyproject.toml")
		if data, readErr := os.ReadFile(src); readErr == nil {
			if err := os.WriteFile(configPath, data, 0o644); err != nil {
				return nil, fmt.Errorf("copy pyproject.toml for %s: %w", e.Target(), err)
			}
		}
	}
	if err := tool.Run(dir, args...); err != nil {
		return nil, fmt.Errorf("format %s: %w", e.Target(), err)
	}
	if tool.Name == "ruff" {
		lintArgs := []string{"check", "--fix"}
		for _, f := range set.Files() {
			lintArgs = append(lintArgs, filepath.Join(dir, filepath.FromSlash(f.Path)))
		}
		if err := tool.Run(dir, lintArgs...); err != nil {
			return nil, fmt.Errorf("lint-fix %s: %w", e.Target(), err)
		}
	}
	formatted := fileset.New()
	for _, f := range set.Files() {
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, err
		}
		if err := formatted.Add(f.Path, content); err != nil {
			return nil, err
		}
	}
	return formatted, nil
}

// Emitters returns every registered emitter in canonical target order.
func Emitters() []emit.Emitter {
	return []emit.Emitter{
		ts.New(),
		publicts.New(),
		publictsweb.New(),
		python.New(),
		publicpython.New(),
		golang.New(),
		publicgo.New(),
		rust.New(),
		publicrust.New(),
	}
}

func emittersFor(targets []emit.Target) []emit.Emitter {
	var out []emit.Emitter
	for _, e := range Emitters() {
		if emit.IncludesTarget(targets, e.Target()) {
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
