// Package naming provides identifier rules shared by SDK emitters and manifests.
package naming

import (
	"strings"
	"unicode"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// LocalName returns the unqualified part of a protobuf name.
func LocalName(fullName string) string {
	return fullName[strings.LastIndex(fullName, ".")+1:]
}

// EnumName returns the explicit generated name or the unqualified proto name.
func EnumName(e *model.Enum) string {
	if e.GeneratedName != "" {
		return e.GeneratedName
	}
	return LocalName(e.FullName)
}

// UpperFirst uppercases the first byte using the existing identifier convention.
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// LowerFirst lowercases the first byte using the existing identifier convention.
func LowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// SnakeCase converts a PascalCase or camelCase identifier to snake_case,
// keeping acronym runs together: "GetAllKeys" becomes "get_all_keys" and
// "CreateObjectAccessURL" becomes "create_object_access_url". Compound
// acronyms stay one word: "InvokeGraphQL" becomes "invoke_graphql".
func SnakeCase(s string) string {
	s = strings.ReplaceAll(s, "GraphQL", "Graphql")
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prevLower := !unicode.IsUpper(runes[i-1]) && runes[i-1] != '_'
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (unicode.IsUpper(runes[i-1]) && nextLower) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
