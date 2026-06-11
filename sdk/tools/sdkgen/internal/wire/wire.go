// Package wire orchestrates the Buf/protoc wire-stub layer. The wire stubs
// remain Buf-generated (a non-goal for sdkgen to replace); sdkgen renders the
// existing templates into scratch directories and then either syncs the
// result into the tree (plain run) or byte-compares against it (--check).
// Both modes share one render path and one staleness definition, so a
// generate always satisfies a subsequent check.
package wire

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/toolchain"
)

// protoRel is the proto module directory, relative to the repo root.
const protoRel = "sdk/proto"

// templates maps each target to its existing buf generate template under
// sdk/proto. Python has no entry: its stubs are vendored with import
// rewriting (see pyvendor.go), mirroring sdk/python/scripts/generate_stubs.py.
var templates = map[emit.Target]string{
	emit.TargetTS:   "buf.typescript.gen.yaml",
	emit.TargetGo:   "buf.go.sdk.gen.yaml",
	emit.TargetRust: "buf.rust.gen.yaml",
}

// staleSuffixes limits wire-stub sync and stale detection to files the
// plugins actually emit; handwritten files living in the same directories
// (such as gestaltd/rpc/protov1/v1/env.go) are out of scope.
var staleSuffixes = map[emit.Target][]string{
	emit.TargetTS:   {"_pb.ts"},
	emit.TargetGo:   {".pb.go"},
	emit.TargetRust: {".rs"},
}

// renderedOut pairs one scratch render with the checked-in tree directory it
// corresponds to.
type renderedOut struct {
	scratchDir string
	treeDir    string
	treeRel    string
	ignore     func(rel string) bool
}

// render renders every selected target's wire stubs into scratch without
// touching the tree.
func render(bufTool, rustfmtTool *toolchain.Tool, repoRoot, scratch string, targets []emit.Target) ([]renderedOut, error) {
	protoDir := filepath.Join(repoRoot, protoRel)
	var outs []renderedOut
	for _, target := range targets {
		if target == emit.TargetPython {
			scratchDir := filepath.Join(scratch, "wire", string(target))
			if err := renderPython(bufTool, protoDir, scratchDir); err != nil {
				return nil, err
			}
			outs = append(outs, renderedOut{
				scratchDir: scratchDir,
				treeDir:    filepath.Join(repoRoot, filepath.FromSlash(pyVendorRel)),
				treeRel:    pyVendorRel,
				ignore:     func(rel string) bool { return !isPythonStub(rel) },
			})
			continue
		}
		scratchTemplate, outsByOrig, err := rewriteTemplate(
			filepath.Join(protoDir, templates[target]),
			filepath.Join(scratch, "wire", string(target)),
		)
		if err != nil {
			return nil, err
		}
		if err := bufTool.Run(protoDir, "generate", "--template", scratchTemplate); err != nil {
			return nil, err
		}
		suffixes := staleSuffixes[target]
		for origOut, scratchDir := range outsByOrig {
			if target == emit.TargetRust {
				if err := rustfmtDir(rustfmtTool, scratchDir); err != nil {
					return nil, err
				}
			}
			treeDir := filepath.Join(protoDir, filepath.FromSlash(origOut))
			treeRel, err := filepath.Rel(repoRoot, treeDir)
			if err != nil {
				return nil, err
			}
			outs = append(outs, renderedOut{
				scratchDir: scratchDir,
				treeDir:    treeDir,
				treeRel:    filepath.ToSlash(treeRel),
				ignore: func(rel string) bool {
					for _, suffix := range suffixes {
						if strings.HasSuffix(rel, suffix) {
							return false
						}
					}
					return true
				},
			})
		}
	}
	return outs, nil
}

// Generate renders the selected targets' wire stubs and syncs them into the
// tree, writing changed files and removing stale ones.
func Generate(bufTool, rustfmtTool *toolchain.Tool, repoRoot, scratch string, targets []emit.Target) error {
	outs, err := render(bufTool, rustfmtTool, repoRoot, scratch, targets)
	if err != nil {
		return err
	}
	for _, out := range outs {
		if err := fileset.SyncDirs(out.scratchDir, out.treeDir, out.ignore); err != nil {
			return err
		}
	}
	return nil
}

// Check renders the selected targets' wire stubs and byte-compares them
// against the tree without mutating it. Drift paths are repo-relative.
func Check(bufTool, rustfmtTool *toolchain.Tool, repoRoot, scratch string, targets []emit.Target) (fileset.Drift, error) {
	outs, err := render(bufTool, rustfmtTool, repoRoot, scratch, targets)
	if err != nil {
		return nil, err
	}
	var drift fileset.Drift
	for _, out := range outs {
		d, err := fileset.CompareDirs(out.scratchDir, out.treeDir, true, out.ignore)
		if err != nil {
			return nil, err
		}
		drift = append(drift, prefixDrift(d, out.treeRel)...)
	}
	return drift, nil
}

func prefixDrift(d fileset.Drift, prefix string) fileset.Drift {
	out := make(fileset.Drift, len(d))
	for i, e := range d {
		out[i] = fileset.DriftEntry{Kind: e.Kind, Path: path.Join(prefix, e.Path)}
	}
	return out
}

// rustfmtDir formats every .rs file under dir with the edition pinned by
// sdk/rust/scripts/generate_stubs.sh.
func rustfmtDir(rustfmtTool *toolchain.Tool, dir string) error {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".rs") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	args := append([]string{"--edition", "2024"}, files...)
	return rustfmtTool.Run(dir, args...)
}
