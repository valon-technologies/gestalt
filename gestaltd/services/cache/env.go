package cache

import "strings"

const DefaultSocketEnv = "GESTALT_CACHE_SOCKET"
const defaultSocketTokenEnvSuffix = "_TOKEN"

func SocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultSocketEnv
	}
	var b strings.Builder
	b.WriteString(DefaultSocketEnv)
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

func SocketTokenEnv(name string) string {
	return SocketEnv(name) + defaultSocketTokenEnvSuffix
}
