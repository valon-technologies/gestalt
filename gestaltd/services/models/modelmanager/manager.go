package modelmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

var ErrInvalidGenerateRequest = errors.New("invalid model generate request")

type Service interface {
	Generate(context.Context, *principal.Principal, coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error)
}

type Config struct {
	Providers       map[string]coremodel.Provider
	DefaultProvider string
}

type Manager struct {
	providers       map[string]coremodel.Provider
	defaultProvider string
}

func New(cfg Config) *Manager {
	providers := make(map[string]coremodel.Provider, len(cfg.Providers))
	for name, provider := range cfg.Providers {
		name = strings.TrimSpace(name)
		if name == "" || provider == nil {
			continue
		}
		providers[name] = provider
	}
	return &Manager{
		providers:       providers,
		defaultProvider: strings.TrimSpace(cfg.DefaultProvider),
	}
}

func (m *Manager) Generate(ctx context.Context, p *principal.Principal, req coremodel.ManagerGenerateRequest) (*coremodel.GenerateResponse, error) {
	if m == nil {
		return nil, fmt.Errorf("model manager is not available")
	}
	providerName := strings.TrimSpace(req.ProviderName)
	if providerName == "" {
		providerName = m.defaultProvider
	}
	if providerName == "" {
		return nil, fmt.Errorf("model provider is required")
	}
	provider := m.providers[providerName]
	if provider == nil {
		return nil, fmt.Errorf("%w: model provider %q", core.ErrNotFound, providerName)
	}
	if err := validateGenerateRequest(req); err != nil {
		return nil, err
	}
	resp, err := provider.Generate(ctx, coremodel.GenerateRequest{
		ProviderName:     providerName,
		Model:            strings.TrimSpace(req.Model),
		Messages:         req.Messages,
		ResponseSchema:   req.ResponseSchema,
		ModelOptions:     req.ModelOptions,
		Metadata:         req.Metadata,
		Subject:          subjectContext(p),
		CallerPluginName: strings.TrimSpace(req.CallerPluginName),
	})
	if err != nil {
		return nil, err
	}
	if len(req.ResponseSchema) > 0 && (resp == nil || resp.StructuredOutput == nil) {
		return nil, fmt.Errorf("model provider did not return structured output")
	}
	return resp, nil
}

func validateGenerateRequest(req coremodel.ManagerGenerateRequest) error {
	if len(req.Messages) == 0 {
		return invalidGenerateRequest("messages are required")
	}
	for i, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		switch role {
		case "system", "user", "assistant":
		default:
			return invalidGenerateRequest("messages[%d].role %q is not supported", i, msg.Role)
		}
		if msg.Text != "" && len(msg.Parts) > 0 {
			return invalidGenerateRequest("messages[%d] must set text or parts, not both", i)
		}
		if msg.Text == "" && len(msg.Parts) == 0 {
			return invalidGenerateRequest("messages[%d] content is required", i)
		}
		for j, part := range msg.Parts {
			if part.Type != "" && part.Type != coremodel.MessagePartTypeText {
				return invalidGenerateRequest("messages[%d].parts[%d].type %q is not supported", i, j, part.Type)
			}
			if part.Text == "" {
				return invalidGenerateRequest("messages[%d].parts[%d].text is required", i, j)
			}
		}
	}
	if len(req.ResponseSchema) > 0 {
		if typ, _ := req.ResponseSchema["type"].(string); strings.TrimSpace(typ) != "object" {
			return invalidGenerateRequest("response_schema.type must be object")
		}
	}
	return nil
}

func invalidGenerateRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidGenerateRequest, fmt.Sprintf(format, args...))
}

func subjectContext(p *principal.Principal) coremodel.SubjectContext {
	p = principal.Canonicalized(p)
	if p == nil {
		return coremodel.SubjectContext{}
	}
	subjectKind := string(p.Kind)
	if subjectKind == "" && p.Identity != nil {
		subjectKind = string(principal.KindUser)
	}
	return coremodel.SubjectContext{
		SubjectID:           p.SubjectID,
		SubjectKind:         subjectKind,
		CredentialSubjectID: p.CredentialSubjectID,
		DisplayName:         p.DisplayName,
		AuthSource:          p.AuthSource(),
	}
}

var _ Service = (*Manager)(nil)
