package providerpkg

import (
	"fmt"
	"os"
	"reflect"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
)

type generatedSourceManifestMetadata struct {
	SecuritySchemes map[string]*providermanifestv1.HTTPSecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	HTTP            map[string]*providermanifestv1.HTTPBinding        `json:"http,omitempty" yaml:"http,omitempty"`
}

func readGeneratedSourceManifestMetadata(path string) (*generatedSourceManifestMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read generated source manifest metadata %q: %w", path, err)
	}
	var metadata generatedSourceManifestMetadata
	if err := decodeStrict(data, ManifestFormatFromPath(path), "generated source manifest metadata", &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

func mergeGeneratedSourceManifestMetadata(spec *providermanifestv1.Spec, metadata *generatedSourceManifestMetadata) {
	if spec == nil || metadata == nil {
		return
	}
	if len(metadata.SecuritySchemes) > 0 {
		merged := cloneGeneratedHTTPSecuritySchemes(metadata.SecuritySchemes)
		for name, scheme := range spec.SecuritySchemes {
			merged[name] = mergeGeneratedHTTPSecurityScheme(merged[name], scheme)
		}
		spec.SecuritySchemes = merged
	}
	if len(metadata.HTTP) > 0 {
		merged := cloneGeneratedHTTPBindings(metadata.HTTP)
		for name, binding := range spec.HTTP {
			merged[name] = mergeGeneratedHTTPBinding(merged[name], binding)
		}
		spec.HTTP = merged
	}
}

func cloneGeneratedHTTPSecuritySchemes(src map[string]*providermanifestv1.HTTPSecurityScheme) map[string]*providermanifestv1.HTTPSecurityScheme {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*providermanifestv1.HTTPSecurityScheme, len(src))
	for name, scheme := range src {
		cloned[name] = cloneGeneratedHTTPSecurityScheme(scheme)
	}
	return cloned
}

func cloneGeneratedHTTPSecurityScheme(src *providermanifestv1.HTTPSecurityScheme) *providermanifestv1.HTTPSecurityScheme {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.Secret = cloneGeneratedHTTPSecretRef(src.Secret)
	return &cloned
}

func cloneGeneratedHTTPSecretRef(src *providermanifestv1.HTTPSecretRef) *providermanifestv1.HTTPSecretRef {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func cloneGeneratedHTTPBindings(src map[string]*providermanifestv1.HTTPBinding) map[string]*providermanifestv1.HTTPBinding {
	if src == nil {
		return nil
	}
	cloned := make(map[string]*providermanifestv1.HTTPBinding, len(src))
	for name, binding := range src {
		cloned[name] = cloneGeneratedHTTPBinding(binding)
	}
	return cloned
}

func cloneGeneratedHTTPBinding(src *providermanifestv1.HTTPBinding) *providermanifestv1.HTTPBinding {
	if src == nil {
		return nil
	}
	cloned := *src
	cloned.RequestBody = cloneGeneratedHTTPRequestBody(src.RequestBody)
	cloned.Ack = cloneGeneratedHTTPAck(src.Ack)
	return &cloned
}

func cloneGeneratedHTTPRequestBody(src *providermanifestv1.HTTPRequestBody) *providermanifestv1.HTTPRequestBody {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Content != nil {
		cloned.Content = make(map[string]*providermanifestv1.HTTPMediaType, len(src.Content))
		for name, mediaType := range src.Content {
			cloned.Content[name] = cloneGeneratedHTTPMediaType(mediaType)
		}
	}
	return &cloned
}

func cloneGeneratedHTTPMediaType(src *providermanifestv1.HTTPMediaType) *providermanifestv1.HTTPMediaType {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func cloneGeneratedHTTPAck(src *providermanifestv1.HTTPAck) *providermanifestv1.HTTPAck {
	if src == nil {
		return nil
	}
	cloned := *src
	if src.Headers != nil {
		cloned.Headers = make(map[string]string, len(src.Headers))
		for name, value := range src.Headers {
			cloned.Headers[name] = value
		}
	}
	cloned.Body = cloneGeneratedHTTPBodyValue(src.Body)
	return &cloned
}

func cloneGeneratedHTTPBodyValue(src any) any {
	if src == nil {
		return nil
	}
	return cloneGeneratedHTTPBodyReflectValue(reflect.ValueOf(src)).Interface()
}

func cloneGeneratedHTTPBodyReflectValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return cloneGeneratedHTTPBodyReflectValue(value.Elem())
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.New(value.Type().Elem())
		cloned.Elem().Set(cloneGeneratedHTTPBodyReflectValue(value.Elem()))
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(cloneGeneratedHTTPBodyReflectValue(iter.Key()), cloneGeneratedHTTPBodyReflectValue(iter.Value()))
		}
		return cloned
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneGeneratedHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneGeneratedHTTPBodyReflectValue(value.Index(i)))
		}
		return cloned
	default:
		return value
	}
}

func mergeGeneratedHTTPSecurityScheme(base, override *providermanifestv1.HTTPSecurityScheme) *providermanifestv1.HTTPSecurityScheme {
	if base == nil {
		return cloneGeneratedHTTPSecurityScheme(override)
	}
	if override == nil {
		return cloneGeneratedHTTPSecurityScheme(base)
	}
	merged := *base
	if override.Type != "" {
		merged.Type = override.Type
	}
	if override.Description != "" {
		merged.Description = override.Description
	}
	if override.SignatureHeader != "" {
		merged.SignatureHeader = override.SignatureHeader
	}
	if override.SignaturePrefix != "" {
		merged.SignaturePrefix = override.SignaturePrefix
	}
	if override.PayloadTemplate != "" {
		merged.PayloadTemplate = override.PayloadTemplate
	}
	if override.TimestampHeader != "" {
		merged.TimestampHeader = override.TimestampHeader
	}
	if override.MaxAgeSeconds != 0 {
		merged.MaxAgeSeconds = override.MaxAgeSeconds
	}
	if override.Name != "" {
		merged.Name = override.Name
	}
	if override.In != "" {
		merged.In = override.In
	}
	if override.Scheme != "" {
		merged.Scheme = override.Scheme
	}
	if override.Secret != nil {
		merged.Secret = cloneGeneratedHTTPSecretRef(override.Secret)
	}
	return &merged
}

func mergeGeneratedHTTPBinding(base, override *providermanifestv1.HTTPBinding) *providermanifestv1.HTTPBinding {
	if base == nil {
		return cloneGeneratedHTTPBinding(override)
	}
	if override == nil {
		return cloneGeneratedHTTPBinding(base)
	}
	merged := *base
	if override.Path != "" {
		merged.Path = override.Path
	}
	if override.Method != "" {
		merged.Method = override.Method
	}
	if override.CredentialMode != "" {
		merged.CredentialMode = override.CredentialMode
	}
	if override.Security != "" {
		merged.Security = override.Security
	}
	if override.Target != "" {
		merged.Target = override.Target
	}
	if override.RequestBody != nil {
		if merged.RequestBody == nil {
			merged.RequestBody = cloneGeneratedHTTPRequestBody(override.RequestBody)
		} else {
			requestBody := *merged.RequestBody
			if override.RequestBody.Required {
				requestBody.Required = true
			}
			if override.RequestBody.Content != nil {
				requestBody.Content = cloneGeneratedHTTPRequestBody(override.RequestBody).Content
			}
			merged.RequestBody = &requestBody
		}
	}
	if override.Ack != nil {
		if merged.Ack == nil {
			merged.Ack = cloneGeneratedHTTPAck(override.Ack)
		} else {
			ack := cloneGeneratedHTTPAck(merged.Ack)
			if override.Ack.Status != 0 {
				ack.Status = override.Ack.Status
			}
			if override.Ack.Headers != nil {
				if ack.Headers == nil {
					ack.Headers = map[string]string{}
				}
				for key, value := range override.Ack.Headers {
					ack.Headers[key] = value
				}
			}
			if override.Ack.Body != nil {
				ack.Body = cloneGeneratedHTTPBodyValue(override.Ack.Body)
			}
			merged.Ack = ack
		}
	}
	return &merged
}
