package agentroute

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("agent route not found")
	ErrConflict = errors.New("agent route conflict")
)

type State string

const (
	StateActive   State = "active"
	StateArchived State = "archived"
)

type Route struct {
	AgentID             string
	OwnerSubjectID      string
	CredentialSubjectID string
	ProviderName        string
	ConfigRevision      string
	AuthorityRef        string
	RequestFingerprint  string
	State               State
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type CreateRequest struct {
	Route
	IdempotencyKey string
}

// Store persists the minimum Gestalt-owned information required to
// reauthorize and route an opaque agent ID. Provider-owned conversation
// history, events, results, and interactions do not belong here.
type Store interface {
	Create(ctx context.Context, req CreateRequest) (route *Route, created bool, err error)
	GetOwned(ctx context.Context, agentID, ownerSubjectID string) (*Route, error)
	FindByIdempotency(ctx context.Context, ownerSubjectID, idempotencyKey string) (*Route, error)
	ListOwned(ctx context.Context, ownerSubjectID string, state State) ([]*Route, error)
	CompareAndSwapRevision(
		ctx context.Context,
		agentID string,
		ownerSubjectID string,
		expectedRevision string,
		nextRevision string,
	) (*Route, error)
	Archive(ctx context.Context, agentID, ownerSubjectID string) (*Route, error)
}

func normalizeCreateRequest(req CreateRequest) (CreateRequest, error) {
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.OwnerSubjectID = strings.TrimSpace(req.OwnerSubjectID)
	req.CredentialSubjectID = strings.TrimSpace(req.CredentialSubjectID)
	req.ProviderName = strings.TrimSpace(req.ProviderName)
	req.ConfigRevision = strings.TrimSpace(req.ConfigRevision)
	req.AuthorityRef = strings.TrimSpace(req.AuthorityRef)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.RequestFingerprint = strings.TrimSpace(req.RequestFingerprint)
	if req.AgentID == "" {
		return CreateRequest{}, fmt.Errorf("agent route requires agent id")
	}
	if req.OwnerSubjectID == "" {
		return CreateRequest{}, fmt.Errorf("agent route requires owner subject id")
	}
	if req.ProviderName == "" {
		return CreateRequest{}, fmt.Errorf("agent route requires provider name")
	}
	if req.ConfigRevision == "" {
		return CreateRequest{}, fmt.Errorf("agent route requires config revision")
	}
	if req.IdempotencyKey != "" && req.RequestFingerprint == "" {
		return CreateRequest{}, fmt.Errorf("idempotent agent route requires request fingerprint")
	}
	switch req.State {
	case "":
		req.State = StateActive
	case StateActive:
	default:
		return CreateRequest{}, fmt.Errorf("new agent route state must be active")
	}
	return req, nil
}

func validState(state State) bool {
	return state == StateActive || state == StateArchived
}

func cloneRoute(route *Route) *Route {
	if route == nil {
		return nil
	}
	cloned := *route
	return &cloned
}
