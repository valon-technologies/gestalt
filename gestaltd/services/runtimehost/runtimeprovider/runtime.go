package runtimeprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const (
	SessionStatePending = "pending"
	SessionStateReady   = "ready"
	SessionStateRunning = "running"
	SessionStateStopped = "stopped"
	SessionStateFailed  = "failed"
)

const (
	defaultListSessionsPageSize = 100
	maxListSessionsPageSize     = 200
)

type listSessionsPageToken struct {
	Version  int    `json:"v"`
	PageSize int    `json:"page_size"`
	LastID   string `json:"last_id,omitempty"`
}

const listSessionsPageTokenVersion = 1

var ErrInvalidListSessionsPagination = errors.New("invalid list sessions pagination")

type HostedAppConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Integration() proto.AppProviderClient
	Close() error
}

type HostedAgentConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Agent() proto.AgentProviderClient
	Close() error
}

type HostedWorkflowConn interface {
	Lifecycle() proto.ProviderLifecycleClient
	Workflow() proto.WorkflowProviderClient
	Close() error
}

type Provider interface {
	Support(ctx context.Context) (*proto.RuntimeSupport, error)
	ListSessions(ctx context.Context, req *proto.ListRuntimeSessionsRequest) (*proto.ListRuntimeSessionsResponse, error)
	StartSession(ctx context.Context, req *proto.StartRuntimeSessionRequest) (*proto.RuntimeSession, error)
	GetSession(ctx context.Context, req *proto.GetRuntimeSessionRequest) (*proto.RuntimeSession, error)
	StopSession(ctx context.Context, req *proto.StopRuntimeSessionRequest) error
	StartApp(ctx context.Context, req *proto.StartHostedAppRequest) (*proto.HostedApp, error)
	Close() error
}

type WorkspaceProvider interface {
	PrepareWorkspace(ctx context.Context, req *proto.PrepareRuntimeWorkspaceRequest) (*proto.PrepareRuntimeWorkspaceResponse, error)
	RemoveWorkspace(ctx context.Context, req *proto.RemoveRuntimeWorkspaceRequest) error
}

func paginateSortedSessionIDs(sessionIDs []string, req *proto.ListRuntimeSessionsRequest) ([]string, string, error) {
	pageSize := int(req.GetPageSize())
	if pageSize < 0 {
		return nil, "", fmt.Errorf("%w: page_size must be non-negative", ErrInvalidListSessionsPagination)
	}
	rawToken := strings.TrimSpace(req.GetPageToken())
	var token listSessionsPageToken
	if rawToken != "" {
		decoded, err := decodeListSessionsPageToken(rawToken)
		if err != nil {
			return nil, "", err
		}
		token = decoded
		if token.Version != listSessionsPageTokenVersion {
			return nil, "", fmt.Errorf("%w: page_token has unsupported version", ErrInvalidListSessionsPagination)
		}
		if token.PageSize <= 0 || token.PageSize > maxListSessionsPageSize {
			return nil, "", fmt.Errorf("%w: page_token has invalid page_size", ErrInvalidListSessionsPagination)
		}
	}
	if pageSize == 0 {
		if rawToken != "" {
			pageSize = token.PageSize
		} else {
			pageSize = defaultListSessionsPageSize
		}
	}
	if pageSize > maxListSessionsPageSize {
		pageSize = maxListSessionsPageSize
	}
	if rawToken != "" && token.PageSize != pageSize {
		return nil, "", fmt.Errorf("%w: page_token does not match page_size", ErrInvalidListSessionsPagination)
	}

	start := 0
	if token.LastID != "" {
		start = sort.Search(len(sessionIDs), func(i int) bool {
			return sessionIDs[i] > token.LastID
		})
	}
	if start >= len(sessionIDs) {
		return nil, "", nil
	}
	end := start + pageSize
	if end > len(sessionIDs) {
		end = len(sessionIDs)
	}
	nextPageToken := ""
	if end < len(sessionIDs) {
		var err error
		nextPageToken, err = encodeListSessionsPageToken(listSessionsPageToken{
			Version:  listSessionsPageTokenVersion,
			PageSize: pageSize,
			LastID:   sessionIDs[end-1],
		})
		if err != nil {
			return nil, "", err
		}
	}
	return sessionIDs[start:end], nextPageToken, nil
}

func encodeListSessionsPageToken(token listSessionsPageToken) (string, error) {
	payload, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("marshal page token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeListSessionsPageToken(raw string) (listSessionsPageToken, error) {
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return listSessionsPageToken{}, fmt.Errorf("%w: page_token is invalid", ErrInvalidListSessionsPagination)
	}
	var token listSessionsPageToken
	if err := json.Unmarshal(payload, &token); err != nil {
		return listSessionsPageToken{}, fmt.Errorf("%w: page_token is invalid", ErrInvalidListSessionsPagination)
	}
	return token, nil
}
