package python

import (
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/emit"
)

func renderPythonClientAlias(alias emit.ClientAlias, base string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "\"\"\"%s\"\"\"\n\n", alias.Doc)
	fmt.Fprintf(&b, "from .%s import *  # noqa: F403\n", base)
	fmt.Fprintf(&b, "from .%s import %s as %s\n\n", base, alias.Source, alias.Target)
	fmt.Fprintf(&b, "__all__ = [\"%s\"]\n", alias.Target)
	return []byte(b.String())
}
