package publicsurface

import (
	"encoding/json"
	"sort"
)

// ManifestField names one REST request field in the golden manifest.
type ManifestField struct {
	Name     string `json:"name"`
	JSONName string `json:"jsonName"`
}

// ManifestEntry is one public unary method in the reviewed golden manifest.
type ManifestEntry struct {
	Service         string          `json:"service"`
	Method          string          `json:"method"`
	StreamingKind   string          `json:"streamingKind"`
	RESTVerb        string          `json:"restVerb,omitempty"`
	RESTPath        string          `json:"restPath,omitempty"`
	RESTBody        string          `json:"restBody,omitempty"`
	RESTPathFields  []ManifestField `json:"restPathFields,omitempty"`
	RESTQueryFields []ManifestField `json:"restQueryFields,omitempty"`
	GRPCPath        string          `json:"grpcPath"`
	Symbols         ManifestSymbols `json:"symbols"`
	Fill            []string        `json:"fill,omitempty"`
	Reject          []string        `json:"reject,omitempty"`
	RequestPolicy   string          `json:"requestPolicy,omitempty"`
}

// Manifest is the deterministic public-surface manifest derived from descriptors.
type Manifest struct {
	Languages       []string        `json:"languages"`
	GRPCMethodCount int             `json:"grpcMethodCount"`
	RESTMethodCount int             `json:"restMethodCount"`
	Methods         []ManifestEntry `json:"methods"`
}

// BuildManifest renders the public unary manifest from a validated view and methods.
func BuildManifest(view *View, methods []PublicMethod) Manifest {
	out := Manifest{
		Languages:       []string{"go", "python", "rust", "typescript"},
		GRPCMethodCount: GRPCMethodCount(view),
		RESTMethodCount: RESTMethodCount(view),
	}
	for _, pm := range methods {
		entry := ManifestEntry{
			Service:       ServiceLocalName(pm.Service),
			Method:        pm.Method,
			StreamingKind: "unary",
			GRPCPath:      pm.FullMethod,
			Fill:          append([]string(nil), FieldNames(pm.ServerFilled)...),
			Reject:        append([]string(nil), FieldNames(pm.Rejected)...),
		}
		if len(entry.Fill) == 0 {
			entry.Fill = nil
		}
		if len(entry.Reject) == 0 {
			entry.Reject = nil
		}
		if pm.Input != nil && (len(entry.Fill) > 0 || len(entry.Reject) > 0) {
			entry.RequestPolicy = "omit"
		}
		if pm.REST != nil {
			entry.RESTVerb = pm.REST.Verb
			entry.RESTPath = pm.REST.PathTemplate
			if pm.REST.Body == BodyStar {
				entry.RESTBody = "*"
			}
			entry.RESTPathFields = manifestFields(pm.REST.PathFields)
			entry.RESTQueryFields = manifestFields(pm.REST.QueryFields)
		}
		entry.Symbols = manifestSymbols(pm)
		out.Methods = append(out.Methods, entry)
	}
	sort.Slice(out.Methods, func(i, j int) bool {
		if out.Methods[i].Service != out.Methods[j].Service {
			return out.Methods[i].Service < out.Methods[j].Service
		}
		return out.Methods[i].Method < out.Methods[j].Method
	})
	return out
}

func manifestFields(fields []PublicField) []ManifestField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]ManifestField, len(fields))
	for i, f := range fields {
		out[i] = ManifestField{Name: f.Name, JSONName: f.JSONName}
	}
	return out
}

// MarshalManifest returns canonical JSON for the manifest golden.
func MarshalManifest(m Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}
