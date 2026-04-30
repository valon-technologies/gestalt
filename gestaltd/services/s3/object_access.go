// Package s3 exposes S3 service primitives.
package s3

import (
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
)

const ObjectAccessPathPrefix = providerhost.S3ObjectAccessPathPrefix

type ObjectAccessURLManager = providerhost.S3ObjectAccessURLManager
type ObjectAccessURLRequest = providerhost.S3ObjectAccessURLRequest
type ObjectAccessTarget = providerhost.S3ObjectAccessTarget

func NewObjectAccessURLManager(secret []byte, baseURL string) (*ObjectAccessURLManager, error) {
	return providerhost.NewS3ObjectAccessURLManager(secret, baseURL)
}

func PluginObjectKey(pluginName, key string) string {
	return providerhost.S3PluginObjectKey(pluginName, key)
}

func NewObjectAccessServer(manager *ObjectAccessURLManager, pluginName, bindingName string) proto.S3ObjectAccessServer {
	return providerhost.NewS3ObjectAccessServer(manager, pluginName, bindingName)
}
