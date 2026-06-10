package rust

import (
	_ "embed"
	"regexp"
	"strings"
)

// supportFile is the public shared generated runtime: the canonical error
// model and the native status representation. It is emitted once, unfiltered,
// as rpc_support.rs.
//
//go:embed rpc_support.rs
var supportFile string

// codecSupportFile holds the crate-private converters between the well-known
// wire types and their native representations. It is emitted as
// codec/support.rs, filtered to the converters some generated module uses.
//
//go:embed codec_support.rs
var codecSupportFile string

// invokeSupportFile holds the public JSON operation-envelope decode runtime.
// It is emitted unfiltered as invoke_support.rs when some method carries a
// json_result annotation.
//
//go:embed invoke_support.rs
var invokeSupportFile string

// hostServiceFile holds the crate-private host-service transport shared by
// the generated clients of host-bound services. It is emitted unfiltered as
// codec/host_service.rs when some service carries a host_binding annotation.
//
//go:embed host_service.rs
var hostServiceFile string

// supportDeps records converter-to-converter calls inside codec_support.rs:
// keeping a converter keeps its callees.
var supportDeps = map[string][]string{
	"to_wire_struct":   {"to_wire_value"},
	"to_wire_value":    {"to_wire_struct"},
	"from_wire_struct": {"from_wire_value"},
	"from_wire_value":  {"from_wire_struct"},
}

// supportFnPattern matches the top-level crate-private converter functions of
// codec_support.rs; every other item in the file is kept unconditionally.
var supportFnPattern = regexp.MustCompile(`(?m)^pub\(crate\) fn (\w+)`)

// renderCodecSupport emits codec/support.rs keeping only the wire converters
// some generated module imports, so every crate-private converter has a
// caller. Items in the embedded file are separated by blank lines and contain
// none, which is what lets the filter split on paragraphs.
func renderCodecSupport(used map[string]bool) string {
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
	for _, chunk := range strings.Split(strings.TrimRight(codecSupportFile, "\n"), "\n\n") {
		if m := supportFnPattern.FindStringSubmatch(chunk); m != nil && !keep[m[1]] {
			continue
		}
		out = append(out, chunk)
	}
	return strings.Join(out, "\n\n") + "\n"
}
