package s3

import "github.com/valon-technologies/gestalt/server/internal/providerenv"

const DefaultSocketEnv = providerenv.DefaultS3SocketEnv

func SocketEnv(name string) string {
	return providerenv.S3SocketEnv(name)
}

func SocketTokenEnv(name string) string {
	return providerenv.S3SocketTokenEnv(name)
}
