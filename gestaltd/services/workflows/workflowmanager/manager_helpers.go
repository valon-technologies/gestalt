package workflowmanager

import (
	"context"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (m *Manager) catalogSelectorConfig() invocation.CatalogSelectorConfig {
	return invocation.CatalogSelectorConfig{
		Invoker:           m.invoker,
		CatalogConnection: m.catalogConnection,
		MCPConnection:     m.mcpConnection,
		DefaultConnection: m.defaultConnection,
	}
}

func workflowSubjectOwnedBy(ownerSubjectID string, p *principal.Principal) bool {
	subjectID := strings.TrimSpace(principalSubjectID(principal.Canonicalized(p)))
	return subjectID != "" && strings.TrimSpace(ownerSubjectID) == subjectID
}

func workflowCallerSubjectID(_ context.Context, reqCtx *proto.RequestContext, p *principal.Principal) (string, error) {
	if id := strings.TrimSpace(reqCtx.GetSubject().GetId()); id != "" {
		return id, nil
	}
	if id := strings.TrimSpace(principal.EffectiveCredentialSubjectID(principal.Canonicalized(p))); id != "" {
		return id, nil
	}
	return "", ErrWorkflowSubjectRequired
}

func workflowSubjectIDFromPrincipal(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.SubjectID)
}

func principalSubjectID(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	return p.SubjectID
}

func (m *Manager) normalizeSignal(ctx context.Context, signal coreworkflow.Signal, reqCtx *proto.RequestContext, p *principal.Principal) (coreworkflow.Signal, error) {
	signal.ID = strings.TrimSpace(signal.ID)
	signal.Name = strings.TrimSpace(signal.Name)
	signal.IdempotencyKey = strings.TrimSpace(signal.IdempotencyKey)
	signal.Payload = maps.Clone(signal.Payload)
	signal.Metadata = maps.Clone(signal.Metadata)
	if signal.Name == "" {
		return coreworkflow.Signal{}, ErrWorkflowSignalNameRequired
	}
	if strings.TrimSpace(signal.CreatedBySubjectID) == "" {
		subjectID, err := workflowCallerSubjectID(ctx, reqCtx, p)
		if err != nil {
			return coreworkflow.Signal{}, err
		}
		signal.CreatedBySubjectID = subjectID
	}
	if signal.CreatedAt == nil || signal.CreatedAt.IsZero() {
		value := m.now().UTC()
		signal.CreatedAt = &value
	} else {
		value := signal.CreatedAt.UTC()
		signal.CreatedAt = &value
	}
	return signal, nil
}

func normalizePublishedEvent(event coreworkflow.Event, now time.Time) coreworkflow.Event {
	event.ID = strings.TrimSpace(event.ID)
	event.Source = strings.TrimSpace(event.Source)
	event.SpecVersion = strings.TrimSpace(event.SpecVersion)
	event.Type = strings.TrimSpace(event.Type)
	event.Subject = strings.TrimSpace(event.Subject)
	event.DataContentType = strings.TrimSpace(event.DataContentType)
	event.Data = maps.Clone(event.Data)
	event.Extensions = maps.Clone(event.Extensions)
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.SpecVersion == "" {
		event.SpecVersion = defaultWorkflowEventSpecVersion
	}
	if event.Time == nil || event.Time.IsZero() {
		value := now.UTC()
		event.Time = &value
	} else {
		value := event.Time.UTC()
		event.Time = &value
	}
	return event
}
