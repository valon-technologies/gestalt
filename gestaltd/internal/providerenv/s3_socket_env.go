package providerenv

import "strings"

const DefaultS3SocketEnv = "GESTALT_S3_SOCKET"
const defaultS3SocketTokenSuffix = "_TOKEN"

func S3SocketEnv(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultS3SocketEnv
	}
	var b strings.Builder
	b.WriteString(DefaultS3SocketEnv)
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

func S3SocketTokenEnv(name string) string {
	return S3SocketEnv(name) + defaultS3SocketTokenSuffix
}
