package rust

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
)

func renderRustClientAlias(alias emit.ClientAlias, base string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "//! %s\n\n", alias.Doc)
	fmt.Fprintf(&b, "pub use crate::%s::%s as %s;\n", base, alias.Source, alias.Target)
	return []byte(b.String())
}
