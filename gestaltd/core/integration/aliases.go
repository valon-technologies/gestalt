package integration

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/plugins/declarative"
)

type AuthHandler = declarative.AuthHandler
type UpstreamAuth = declarative.UpstreamAuth
type AuthStyle = declarative.AuthStyle

const (
	AuthStyleBearer = declarative.AuthStyleBearer
	AuthStyleRaw    = declarative.AuthStyleRaw
	AuthStyleNone   = declarative.AuthStyleNone
	AuthStyleBasic  = declarative.AuthStyleBasic
)

type Base = declarative.Base
type ResponseMappingConfig = declarative.ResponseMappingConfig
type PaginationProjectionConfig = declarative.PaginationProjectionConfig
type Restricted = declarative.Restricted
type RestrictedOption = declarative.RestrictedOption

func WithDescriptions(descs map[string]string) RestrictedOption {
	return declarative.WithDescriptions(descs)
}

func WithAllowedRoles(roles map[string][]string) RestrictedOption {
	return declarative.WithAllowedRoles(roles)
}

func WithTags(tags map[string][]string) RestrictedOption {
	return declarative.WithTags(tags)
}

func NewRestricted(inner core.Provider, ops map[string]string, opts ...RestrictedOption) core.Provider {
	return declarative.NewRestricted(inner, ops, opts...)
}

func ConvertParameters(params []catalog.CatalogParameter) []core.Parameter {
	return declarative.ConvertParameters(params)
}

func OperationsList(c *catalog.Catalog) []core.Operation {
	return declarative.OperationsList(c)
}

func SynthesizeInputSchema(params []catalog.CatalogParameter) json.RawMessage {
	return declarative.SynthesizeInputSchema(params)
}

func NormalizeType(t string) string {
	return declarative.NormalizeType(t)
}

func AnnotationsFromMethod(method string) catalog.OperationAnnotations {
	return declarative.AnnotationsFromMethod(method)
}

func CompileSchemas(c *catalog.Catalog) {
	declarative.CompileSchemas(c)
}
