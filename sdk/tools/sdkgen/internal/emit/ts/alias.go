package ts

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
)

func renderTSClientAlias(alias emit.ClientAlias, base string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "/**\n * %s\n *\n * @module services/%s\n */\n\n", alias.Doc, strings.ToLower(alias.Target))
	fmt.Fprintf(&b, "export * from \"../%s.ts\";\n\n", base)
	fmt.Fprintf(&b, "/** Canonical client for the gestalt.provider.v1.Authentication wire service. */\n")
	fmt.Fprintf(&b, "export { %s as %s } from \"../%s.ts\";\n", alias.Source, alias.Target, base)
	return []byte(b.String())
}
