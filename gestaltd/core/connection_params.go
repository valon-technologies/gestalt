package core

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateConnectionParamDefs enforces the account-identity contract at the
// provider boundary. A connection may declare no stable identity, but it may
// declare at most one, and that identity must come from the OAuth token
// response rather than user input or discovery.
func ValidateConnectionParamDefs(defs map[string]ConnectionParamDef) error {
	var identityNames []string
	for name, def := range defs {
		if !def.AccountIdentity {
			continue
		}
		if strings.TrimSpace(def.From) != "token_response" {
			return fmt.Errorf("connection parameter %q marked accountIdentity must use from: token_response", name)
		}
		if strings.TrimSpace(def.Field) == "" {
			return fmt.Errorf("connection parameter %q marked accountIdentity must declare a token response field", name)
		}
		identityNames = append(identityNames, name)
	}
	if len(identityNames) > 1 {
		sort.Strings(identityNames)
		return fmt.Errorf("connection declares multiple accountIdentity parameters: %s", strings.Join(identityNames, ", "))
	}
	return nil
}
