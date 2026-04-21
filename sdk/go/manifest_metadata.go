package gestalt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const envWriteManifestMetadata = "GESTALT_PLUGIN_WRITE_MANIFEST_METADATA"

// ManifestMetadata describes the source-generated plugin manifest fragment that
// should augment manifest.yaml during source builds and provider release.
type ManifestMetadata struct {
	SecuritySchemes map[string]*WebhookSecurityScheme `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	Webhooks        map[string]*WebhookDef            `json:"webhooks,omitempty" yaml:"webhooks,omitempty"`
}

type WebhookDef struct {
	Summary     string            `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Path        string            `json:"path,omitempty" yaml:"path,omitempty"`
	Get         *WebhookOperation `json:"get,omitempty" yaml:"get,omitempty"`
	Post        *WebhookOperation `json:"post,omitempty" yaml:"post,omitempty"`
	Put         *WebhookOperation `json:"put,omitempty" yaml:"put,omitempty"`
	Delete      *WebhookOperation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Target      *WebhookTarget    `json:"target,omitempty" yaml:"target,omitempty"`
	Execution   *WebhookExecution `json:"execution,omitempty" yaml:"execution,omitempty"`
}

type WebhookOperation struct {
	OperationID string                      `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Summary     string                      `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description string                      `json:"description,omitempty" yaml:"description,omitempty"`
	RequestBody *WebhookRequestBody         `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses   map[string]*WebhookResponse `json:"responses,omitempty" yaml:"responses,omitempty"`
	Security    []SecurityRequirement       `json:"security,omitempty" yaml:"security,omitempty"`
}

type WebhookRequestBody struct {
	Required bool                         `json:"required,omitempty" yaml:"required,omitempty"`
	Content  map[string]*WebhookMediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

type WebhookMediaType struct {
	Schema any `json:"schema,omitempty" yaml:"schema,omitempty"`
}

type WebhookResponse struct {
	Description string                       `json:"description,omitempty" yaml:"description,omitempty"`
	Headers     map[string]string            `json:"headers,omitempty" yaml:"headers,omitempty"`
	Content     map[string]*WebhookMediaType `json:"content,omitempty" yaml:"content,omitempty"`
	Body        any                          `json:"body,omitempty" yaml:"body,omitempty"`
}

type SecurityRequirement map[string][]string

type WebhookTarget struct {
	Operation string                 `json:"operation,omitempty" yaml:"operation,omitempty"`
	Workflow  *WebhookWorkflowTarget `json:"workflow,omitempty" yaml:"workflow,omitempty"`
}

type WebhookWorkflowTarget struct {
	Provider   string         `json:"provider,omitempty" yaml:"provider,omitempty"`
	Plugin     string         `json:"plugin,omitempty" yaml:"plugin,omitempty"`
	Operation  string         `json:"operation,omitempty" yaml:"operation,omitempty"`
	Connection string         `json:"connection,omitempty" yaml:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty" yaml:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty" yaml:"input,omitempty"`
}

type WebhookExecution struct {
	Mode             string `json:"mode,omitempty" yaml:"mode,omitempty"`
	AcceptedResponse string `json:"acceptedResponse,omitempty" yaml:"acceptedResponse,omitempty"`
}

type WebhookSecurityScheme struct {
	Type        string           `json:"type,omitempty" yaml:"type,omitempty"`
	Description string           `json:"description,omitempty" yaml:"description,omitempty"`
	Name        string           `json:"name,omitempty" yaml:"name,omitempty"`
	In          string           `json:"in,omitempty" yaml:"in,omitempty"`
	Scheme      string           `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Secret      *SecretRef       `json:"secret,omitempty" yaml:"secret,omitempty"`
	Signature   *SignatureConfig `json:"signature,omitempty" yaml:"signature,omitempty"`
	Replay      *ReplayConfig    `json:"replay,omitempty" yaml:"replay,omitempty"`
	MTLS        *MTLSConfig      `json:"mtls,omitempty" yaml:"mtls,omitempty"`
}

type SecretRef struct {
	Env    string `json:"env,omitempty" yaml:"env,omitempty"`
	Secret string `json:"secret,omitempty" yaml:"secret,omitempty"`
}

type SignatureConfig struct {
	Algorithm        string `json:"algorithm,omitempty" yaml:"algorithm,omitempty"`
	SignatureHeader  string `json:"signatureHeader,omitempty" yaml:"signatureHeader,omitempty"`
	TimestampHeader  string `json:"timestampHeader,omitempty" yaml:"timestampHeader,omitempty"`
	DeliveryIDHeader string `json:"deliveryIdHeader,omitempty" yaml:"deliveryIdHeader,omitempty"`
	PayloadTemplate  string `json:"payloadTemplate,omitempty" yaml:"payloadTemplate,omitempty"`
	DigestPrefix     string `json:"digestPrefix,omitempty" yaml:"digestPrefix,omitempty"`
}

type ReplayConfig struct {
	MaxAge string `json:"maxAge,omitempty" yaml:"maxAge,omitempty"`
}

type MTLSConfig struct {
	SubjectAltName string `json:"subjectAltName,omitempty" yaml:"subjectAltName,omitempty"`
}

func cloneManifestMetadata(src *ManifestMetadata) *ManifestMetadata {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		panic(fmt.Sprintf("clone manifest metadata: %v", err))
	}
	var dst ManifestMetadata
	if err := json.Unmarshal(data, &dst); err != nil {
		panic(fmt.Sprintf("clone manifest metadata: %v", err))
	}
	return &dst
}

func writeManifestMetadataJSON(metadata *ManifestMetadata, path string) error {
	if metadata == nil {
		metadata = &ManifestMetadata{}
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest metadata: %w", err)
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create manifest metadata directory %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write manifest metadata %q: %w", path, err)
	}
	return nil
}
