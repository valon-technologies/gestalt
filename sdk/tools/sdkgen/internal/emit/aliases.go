// Package emit defines shared client alias generation for sdkgen.
package emit

// ClientAlias renames a generated wire service client for public SDK output.
type ClientAlias struct {
	// Source is the proto service name, e.g. Authentication.
	Source string
	// Target is the exported client name, e.g. Identity.
	Target string
	// Doc describes the renamed client.
	Doc string
}

// ServiceClientAliases lists generated host-service clients that expose a
// canonical alias over an existing wire service.
var ServiceClientAliases = []ClientAlias{
	{
		Source: "Authentication",
		Target: "Identity",
		Doc:    "Identity is the canonical client for the gestalt.provider.v1.Authentication wire service.",
	},
}

// ClientAliasFor returns the alias definition for source, or nil when none.
func ClientAliasFor(source string) *ClientAlias {
	for i := range ServiceClientAliases {
		if ServiceClientAliases[i].Source == source {
			return &ServiceClientAliases[i]
		}
	}
	return nil
}
