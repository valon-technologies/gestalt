// Package s3 exposes S3 provider transport and object-access primitives.
package s3

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

const DefaultSocketEnv = providerhost.DefaultS3SocketEnv
const ObjectAccessPathPrefix = providerhost.S3ObjectAccessPathPrefix

type ExecConfig = providerhost.S3ExecConfig
type ServerOptions = providerhost.S3ServerOptions
type ObjectAccessURLManager = providerhost.S3ObjectAccessURLManager
type ObjectAccessURLRequest = providerhost.S3ObjectAccessURLRequest
type ObjectAccessTarget = providerhost.S3ObjectAccessTarget

func SocketEnv(name string) string {
	return providerhost.S3SocketEnv(name)
}

func SocketTokenEnv(name string) string {
	return providerhost.S3SocketTokenEnv(name)
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (s3store.Client, error) {
	return providerhost.NewExecutableS3(ctx, cfg)
}

func NewServer(client s3store.Client, pluginName string) proto.S3Server {
	return providerhost.NewS3Server(client, pluginName)
}

func NewServerWithOptions(client s3store.Client, pluginName string, opts ServerOptions) proto.S3Server {
	return providerhost.NewS3ServerWithOptions(client, pluginName, opts)
}

func NewObjectAccessURLManager(secret []byte, baseURL string) (*ObjectAccessURLManager, error) {
	return providerhost.NewS3ObjectAccessURLManager(secret, baseURL)
}

func PluginObjectKey(pluginName, key string) string {
	return providerhost.S3PluginObjectKey(pluginName, key)
}

func NewObjectAccessServer(manager *ObjectAccessURLManager, pluginName, bindingName string) proto.S3ObjectAccessServer {
	return providerhost.NewS3ObjectAccessServer(manager, pluginName, bindingName)
}
