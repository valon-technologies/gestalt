package providerhost

import "strings"

const DefaultIndexedDBSocketEnv = "GESTALT_INDEXEDDB_SOCKET"

func IndexedDBSocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultIndexedDBSocketEnv
	}
	var b strings.Builder
	b.WriteString(DefaultIndexedDBSocketEnv)
	b.WriteByte('_')
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
