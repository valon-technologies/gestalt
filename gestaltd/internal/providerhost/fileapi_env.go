package providerhost

import "strings"

const DefaultFileAPISocketEnv = "GESTALT_FILEAPI_SOCKET"

func FileAPISocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultFileAPISocketEnv
	}
	var b strings.Builder
	b.WriteString(DefaultFileAPISocketEnv)
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
