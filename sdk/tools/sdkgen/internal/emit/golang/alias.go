package golang

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
)

func renderGoClientAlias(alias emit.ClientAlias) []byte {
	var b strings.Builder
	b.WriteString("package client\n\n")
	fmt.Fprintf(&b, "// %s\n", alias.Doc)
	fmt.Fprintf(&b, "// Authentication is deprecated; use %s for new code.\n", alias.Target)
	fmt.Fprintf(&b, "type %s = %s\n\n", alias.Target, alias.Source)
	fmt.Fprintf(&b, "// New%s creates a %s client over an injected gRPC connection.\n", alias.Target, alias.Target)
	fmt.Fprintf(&b, "var New%s = New%s\n\n", alias.Target, alias.Source)
	fmt.Fprintf(&b, "// Connect%s dials the authentication host service and returns the canonical %s client.\n", alias.Target, alias.Target)
	fmt.Fprintf(&b, "var Connect%s = Connect%s\n", alias.Target, alias.Source)
	return []byte(b.String())
}
