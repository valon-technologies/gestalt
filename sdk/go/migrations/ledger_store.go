package migrations

import (
	"regexp"
	"strings"
)

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// ProviderLedgerStore returns the default ledger store for a provider name.
func ProviderLedgerStore(providerName string) string {
	normalized := strings.TrimSpace(providerName)
	if at := strings.LastIndex(normalized, "/"); at >= 0 {
		normalized = normalized[at+1:]
	}
	if normalized == "" {
		return defaultLedgerStore
	}
	slug := slugName(normalized)
	if slug == "" {
		return defaultLedgerStore
	}
	snake := strings.ToLower(strings.ReplaceAll(camelBoundary.ReplaceAllString(slug, `${1}_${2}`), "-", "_"))
	return snake + "_migrations"
}

func slugName(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
