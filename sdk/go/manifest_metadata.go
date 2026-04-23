package gestalt

import (
	"bytes"
	"fmt"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

// ManifestMetadata describes optional manifest-backed metadata generated from
// Go-defined plugin routers.
type ManifestMetadata struct {
	SecuritySchemes map[string]HTTPSecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	HTTP            map[string]HTTPBinding        `json:"http,omitempty" yaml:"http,omitempty"`
}

// HTTPSecuritySchemeType identifies the supported hosted HTTP auth schemes.
type HTTPSecuritySchemeType string

const (
	HTTPSecuritySchemeTypeHMAC   HTTPSecuritySchemeType = "hmac"
	HTTPSecuritySchemeTypeAPIKey HTTPSecuritySchemeType = "apiKey"
	HTTPSecuritySchemeTypeHTTP   HTTPSecuritySchemeType = "http"
	HTTPSecuritySchemeTypeNone   HTTPSecuritySchemeType = "none"
)

// HTTPIn identifies where an HTTP auth value is supplied.
type HTTPIn string

const (
	HTTPInHeader HTTPIn = "header"
	HTTPInQuery  HTTPIn = "query"
)

// HTTPAuthScheme identifies the HTTP auth scheme for `type: http`.
type HTTPAuthScheme string

const (
	HTTPAuthSchemeBasic  HTTPAuthScheme = "basic"
	HTTPAuthSchemeBearer HTTPAuthScheme = "bearer"
)

// HTTPSecretRef points at the secret backing a hosted HTTP security scheme.
type HTTPSecretRef struct {
	Env    string `json:"env,omitempty" yaml:"env,omitempty"`
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
}

// HTTPSecurityScheme describes one named hosted HTTP security scheme.
type HTTPSecurityScheme struct {
	Type            HTTPSecuritySchemeType `json:"type,omitempty" yaml:"type,omitempty"`
	Description     string                 `json:"description,omitempty" yaml:"description,omitempty"`
	SignatureHeader string                 `json:"signatureHeader,omitempty" yaml:"signatureHeader,omitempty"`
	SignaturePrefix string                 `json:"signaturePrefix,omitempty" yaml:"signaturePrefix,omitempty"`
	PayloadTemplate string                 `json:"payloadTemplate,omitempty" yaml:"payloadTemplate,omitempty"`
	TimestampHeader string                 `json:"timestampHeader,omitempty" yaml:"timestampHeader,omitempty"`
	MaxAgeSeconds   int                    `json:"maxAgeSeconds,omitempty" yaml:"maxAgeSeconds,omitempty"`
	Name            string                 `json:"name,omitempty" yaml:"name,omitempty"`
	In              HTTPIn                 `json:"in,omitempty" yaml:"in,omitempty"`
	Scheme          HTTPAuthScheme         `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Secret          *HTTPSecretRef         `json:"secret,omitempty" yaml:"secret,omitempty"`
}

// HTTPBinding describes one hosted HTTP endpoint bound to a registered
// operation target.
type HTTPBinding struct {
	Path           string           `json:"path" yaml:"path"`
	Method         string           `json:"method" yaml:"method"`
	CredentialMode string           `json:"credentialMode,omitempty" yaml:"credentialMode,omitempty"`
	RequestBody    *HTTPRequestBody `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Security       string           `json:"security,omitempty" yaml:"security,omitempty"`
	Target         string           `json:"target" yaml:"target"`
	Ack            *HTTPAck         `json:"ack,omitempty" yaml:"ack,omitempty"`
}

// HTTPRequestBody describes the accepted content types for a hosted HTTP
// binding.
type HTTPRequestBody struct {
	Required bool                     `json:"required,omitempty" yaml:"required,omitempty"`
	Content  map[string]HTTPMediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

// HTTPMediaType reserves per-content-type metadata for future expansion.
type HTTPMediaType struct{}

// HTTPAck describes the immediate host-managed response for a hosted HTTP
// binding.
type HTTPAck struct {
	Status  int               `json:"status,omitempty" yaml:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	Body    any               `json:"body,omitempty" yaml:"body,omitempty"`
}

func hasManifestMetadata(metadata ManifestMetadata) bool {
	return len(metadata.SecuritySchemes) > 0 || len(metadata.HTTP) > 0
}

func cloneManifestMetadataValue(metadata ManifestMetadata) *ManifestMetadata {
	return cloneManifestMetadata(&metadata)
}

func cloneManifestMetadata(metadata *ManifestMetadata) *ManifestMetadata {
	if metadata == nil {
		return nil
	}
	cloned := &ManifestMetadata{}
	if len(metadata.SecuritySchemes) > 0 {
		cloned.SecuritySchemes = make(map[string]HTTPSecurityScheme, len(metadata.SecuritySchemes))
		for name, scheme := range metadata.SecuritySchemes {
			cloned.SecuritySchemes[name] = cloneHTTPSecurityScheme(scheme)
		}
	}
	if len(metadata.HTTP) > 0 {
		cloned.HTTP = make(map[string]HTTPBinding, len(metadata.HTTP))
		for name, binding := range metadata.HTTP {
			cloned.HTTP[name] = cloneHTTPBinding(binding)
		}
	}
	if !hasManifestMetadata(*cloned) {
		return nil
	}
	return cloned
}

func cloneHTTPSecurityScheme(scheme HTTPSecurityScheme) HTTPSecurityScheme {
	cloned := scheme
	cloned.Secret = cloneHTTPSecretRef(scheme.Secret)
	return cloned
}

func cloneHTTPSecretRef(secret *HTTPSecretRef) *HTTPSecretRef {
	if secret == nil {
		return nil
	}
	cloned := *secret
	return &cloned
}

func cloneHTTPBinding(binding HTTPBinding) HTTPBinding {
	cloned := binding
	cloned.RequestBody = cloneHTTPRequestBody(binding.RequestBody)
	cloned.Ack = cloneHTTPAck(binding.Ack)
	return cloned
}

func cloneHTTPRequestBody(body *HTTPRequestBody) *HTTPRequestBody {
	if body == nil {
		return nil
	}
	cloned := &HTTPRequestBody{
		Required: body.Required,
	}
	if len(body.Content) > 0 {
		cloned.Content = make(map[string]HTTPMediaType, len(body.Content))
		for name, mediaType := range body.Content {
			cloned.Content[name] = mediaType
		}
	}
	return cloned
}

func cloneHTTPAck(ack *HTTPAck) *HTTPAck {
	if ack == nil {
		return nil
	}
	cloned := &HTTPAck{
		Status: ack.Status,
		Body:   cloneHTTPBodyValue(ack.Body),
	}
	if len(ack.Headers) > 0 {
		cloned.Headers = make(map[string]string, len(ack.Headers))
		for name, value := range ack.Headers {
			cloned.Headers[name] = value
		}
	}
	return cloned
}

func cloneHTTPBodyValue(value any) any {
	if value == nil {
		return nil
	}
	return cloneHTTPBodyReflectValue(reflect.ValueOf(value)).Interface()
}

func cloneHTTPBodyReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return cloneHTTPBodyReflectValue(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneHTTPBodyReflectValue(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(cloneHTTPBodyReflectValue(iter.Key()), cloneHTTPBodyReflectValue(iter.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	default:
		return value
	}
}

func writeManifestMetadataYAML(metadata ManifestMetadata, path string) error {
	if !hasManifestMetadata(metadata) {
		return nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(metadata); err != nil {
		return fmt.Errorf("encode manifest metadata YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close YAML encoder: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
