package providerhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	s3service "github.com/valon-technologies/gestalt/server/services/s3"
)

type S3ExecConfig = s3service.ExecConfig
type S3ServerOptions = s3service.ServerOptions
type S3ObjectAccessURLManager = s3service.ObjectAccessURLManager
type S3ObjectAccessURLRequest = s3service.ObjectAccessURLRequest
type S3ObjectAccessTarget = s3service.ObjectAccessTarget

const S3ObjectAccessPathPrefix = s3service.ObjectAccessPathPrefix

func NewExecutableS3(ctx context.Context, cfg S3ExecConfig) (s3store.Client, error) {
	return s3service.NewExecutable(ctx, cfg)
}

func NewS3Server(client s3store.Client, pluginName string) proto.S3Server {
	return s3service.NewServer(client, pluginName)
}

func NewS3ServerWithOptions(client s3store.Client, pluginName string, opts S3ServerOptions) proto.S3Server {
	return s3service.NewServerWithOptions(client, pluginName, opts)
}

func NewS3ObjectAccessURLManager(secret []byte, baseURL string) (*S3ObjectAccessURLManager, error) {
	return s3service.NewObjectAccessURLManager(secret, baseURL)
}

func S3PluginObjectKey(pluginName, key string) string {
	return s3service.PluginObjectKey(pluginName, key)
}

func NewS3ObjectAccessServer(manager *S3ObjectAccessURLManager, pluginName, bindingName string) proto.S3ObjectAccessServer {
	return s3service.NewObjectAccessServer(manager, pluginName, bindingName)
}
