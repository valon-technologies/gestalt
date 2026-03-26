package integration

import (
	"github.com/valon-technologies/gestalt/core"
	"github.com/valon-technologies/gestalt/core/catalog"
	coreintegration "github.com/valon-technologies/gestalt/core/integration"
)

type AuthHandler = coreintegration.AuthHandler
type UpstreamAuth = coreintegration.UpstreamAuth
type Endpoint = coreintegration.Endpoint
type AuthStyle = coreintegration.AuthStyle
type Base = coreintegration.Base

const (
	AuthStyleBearer = coreintegration.AuthStyleBearer
	AuthStyleRaw    = coreintegration.AuthStyleRaw
	AuthStyleNone   = coreintegration.AuthStyleNone
	AuthStyleBasic  = coreintegration.AuthStyleBasic
)

func OperationsList(c *catalog.Catalog) []core.Operation {
	return coreintegration.OperationsList(c)
}

func EndpointsMap(c *catalog.Catalog) (map[string]Endpoint, error) {
	return coreintegration.EndpointsMap(c)
}

func QueriesMap(c *catalog.Catalog) map[string]string {
	return coreintegration.QueriesMap(c)
}

func NormalizeType(t string) string {
	return coreintegration.NormalizeType(t)
}

func AnnotationsFromMethod(method string) catalog.OperationAnnotations {
	return coreintegration.AnnotationsFromMethod(method)
}

func CompileSchemas(c *catalog.Catalog) {
	coreintegration.CompileSchemas(c)
}
