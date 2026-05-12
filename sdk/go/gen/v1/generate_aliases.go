//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
)

const internalImport = "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: go run ./sdk/go/gen/v1/generate_aliases.go <internal-gen-dir> <output-file>\n")
		os.Exit(2)
	}

	internalDir := os.Args[1]
	outputFile := os.Args[2]

	consts, types, vars, err := exportedNames(internalDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate aliases: %v\n", err)
		os.Exit(1)
	}

	var buf bytes.Buffer
	buf.WriteString("// Code generated for compatibility; DO NOT EDIT.\n")
	buf.WriteString("// This package preserves the public generated-protobuf import path while the SDK uses github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1.\n")
	buf.WriteString("package proto\n\n")
	buf.WriteString("import internal \"")
	buf.WriteString(internalImport)
	buf.WriteString("\"\n\n")
	writeAliasBlock(&buf, "const", consts)
	writeAliasBlock(&buf, "type", types)
	writeAliasBlock(&buf, "var", vars)

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		fmt.Fprintf(os.Stderr, "format aliases: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outputFile, formatted, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write aliases: %v\n", err)
		os.Exit(1)
	}
}

func exportedNames(dir string) (consts []string, types []string, vars []string, err error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, filepath.Clean(dir), nil, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	pkg, ok := pkgs["proto"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("package proto not found in %s", dir)
	}

	seenConsts := map[string]bool{}
	seenTypes := map[string]bool{}
	seenVars := map[string]bool{}

	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if !name.IsExported() {
								continue
							}
							switch decl.Tok {
							case token.CONST:
								if !seenConsts[name.Name] {
									seenConsts[name.Name] = true
									consts = append(consts, name.Name)
								}
							case token.VAR:
								if !seenVars[name.Name] {
									seenVars[name.Name] = true
									vars = append(vars, name.Name)
								}
							}
						}
					case *ast.TypeSpec:
						if spec.Name.IsExported() && !seenTypes[spec.Name.Name] {
							seenTypes[spec.Name.Name] = true
							types = append(types, spec.Name.Name)
						}
					}
				}
			case *ast.FuncDecl:
				if decl.Recv == nil && decl.Name.IsExported() && !seenVars[decl.Name.Name] {
					seenVars[decl.Name.Name] = true
					vars = append(vars, decl.Name.Name)
				}
			}
		}
	}

	sort.Strings(consts)
	sort.Strings(types)
	sort.Strings(vars)
	return consts, types, vars, nil
}

func writeAliasBlock(buf *bytes.Buffer, kind string, names []string) {
	if len(names) == 0 {
		return
	}

	buf.WriteString(kind)
	buf.WriteString(" (\n")
	for _, name := range names {
		buf.WriteByte('\t')
		buf.WriteString(name)
		buf.WriteString(" = internal.")
		buf.WriteString(name)
		buf.WriteByte('\n')
	}
	buf.WriteString(")\n\n")
}
