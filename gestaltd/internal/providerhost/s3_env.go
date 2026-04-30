package providerhost

import s3service "github.com/valon-technologies/gestalt/server/services/s3"

const DefaultS3SocketEnv = s3service.DefaultSocketEnv

func S3SocketEnv(name string) string {
	return s3service.SocketEnv(name)
}

func S3SocketTokenEnv(name string) string {
	return s3service.SocketTokenEnv(name)
}
