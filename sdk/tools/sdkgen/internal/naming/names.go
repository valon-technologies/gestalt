// Package naming provides identifier rules shared by SDK emitters and manifests.
package naming

import (
	"strings"

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
