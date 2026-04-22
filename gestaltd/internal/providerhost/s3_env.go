package providerhost

import "github.com/valon-technologies/gestalt/server/internal/providerenv"

const DefaultS3SocketEnv = providerenv.DefaultS3SocketEnv

func S3SocketEnv(name string) string {
	return providerenv.S3SocketEnv(name)
}

func S3SocketTokenEnv(name string) string {
	return providerenv.S3SocketTokenEnv(name)
}
