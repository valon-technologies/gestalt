package rust

import (
	_ "embed"
	"regexp"
	"strings"
)

// supportFile is the shared generated runtime: the canonical error model,
// native representations for well-known types, and the conversions every
// generated client uses. It is emitted once as rpc_support.rs.
//
//go:embed rpc_support.rs
var supportFile string

// supportDeps records converter-to-converter calls inside rpc_support.rs:
// keeping a converter keeps its callees.
var supportDeps = map[string][]string{
	"to_wire_struct":   {"to_wire_value"},
	"to_wire_value":    {"to_wire_struct"},
	"from_wire_struct": {"from_wire_value"},
	"from_wire_value":  {"from_wire_struct"},
}

// supportFnPattern matches the top-level crate-private converter functions of
// rpc_support.rs; every other item in the file is part of the public surface.
var supportFnPattern = regexp.MustCompile(`(?m)^pub\(crate\) fn (\w+)`)

// renderSupport emits rpc_support.rs keeping only the wire converters some
// generated file imports, so every crate-private converter has a caller. Items
// in the embedded file are separated by blank lines and contain none, which is
// what lets the filter split on paragraphs.
func renderSupport(used map[string]bool) string {
	keep := map[string]bool{}
	var add func(name string)
	add = func(name string) {
		if keep[name] {
			return
		}
		keep[name] = true
		for _, dep := range supportDeps[name] {
			add(dep)
		}
	}
	for name := range used {
		add(name)
	}

	var out []string
	for _, chunk := range strings.Split(strings.TrimRight(supportFile, "\n"), "\n\n") {
		if m := supportFnPattern.FindStringSubmatch(chunk); m != nil && !keep[m[1]] {
			continue
		}
		out = append(out, chunk)
	}
	return strings.Join(out, "\n\n") + "\n"
}
