package runtimeprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type SessionState string

const (
	SessionStatePending SessionState = "pending"
	SessionStateReady   SessionState = "ready"
	SessionStateRunning SessionState = "running"
	SessionStateStopped SessionState = "stopped"
	SessionStateFailed  SessionState = "failed"
)

// PolicyAction mirrors the host egress default for runtime-launched apps.
// Runtime backends live outside gestaltd internals, so this contract cannot
// depend on the server's internal egress package directly.
type PolicyAction string

const (
	PolicyAllow PolicyAction = "allow"
	PolicyDeny  PolicyAction = "deny"
)

type EgressMode string

const (
	EgressModeNone     EgressMode = "none"
	EgressModeCIDR     EgressMode = "cidr"
	EgressModeHostname EgressMode = "hostname"
)

type Support struct {
	CanHostApps                bool
	EgressMode                 EgressMode
	SupportsPrepareWorkspace   bool
	SupportsDirectHostServices bool
}

type Session struct {
	ID           string
	State        SessionState
	Metadata     map[string]string
	Lifecycle    *SessionLifecycle
	StateReason  string
	StateMessage string
}

type SessionLifecycle struct {
	StartedAt          *time.Time
	RecommendedDrainAt *time.Time
	ExpiresAt          *time.Time
}

type StartSessionRequest struct {
	AppName       string
	Template      string
	Image         string
	ImagePullAuth *ImagePullAuth
	Metadata      map[string]string
}

type ImagePullAuth struct {
	DockerConfigJSON string
}

type GetSessionRequest struct {
	SessionID string
}

type StopSessionRequest struct {
	SessionID string
}

type ListSessionsRequest struct {
	PageSize  int
	PageToken string
}

type ListSessionsResponse struct {
	Sessions      []Session
	NextPageToken string
}

const (
	DefaultListSessionsPageSize = 100
	MaxListSessionsPageSize     = 200
)

type normalizedListSessionsRequest struct {
	pageSize int
	lastID   string
}

type listSessionsPageToken struct {
	Version  int    `json:"v"`
	PageSize int    `json:"page_size"`
	LastID   string `json:"last_id,omitempty"`
}

const listSessionsPageTokenVersion = 1

var ErrInvalidListSessionsPagination = errors.New("invalid list sessions pagination")

type Workspace struct {
	Checkouts []WorkspaceGitCheckout
	CWD       string
}

type WorkspaceGitCheckout struct {
	URL  string
	Ref  string
	Path string
}

type PreparedWorkspace struct {
	Root string
	CWD  string
}

type PrepareWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
	Workspace      *Workspace
}

type RemoveWorkspaceRequest struct {
	SessionID      string
	AgentSessionID string
}

// StartAppRequest describes the app process to launch inside a runtime
// session. Implementations own allocation and injection of the app's
// provider listener endpoint and must return a host-reachable dial target in
// HostedApp.DialTarget.
type StartAppRequest struct {
	SessionID    string
	AppName      string
	Command      string
	Args         []string
	Workdir      string
	Env          map[string]string
	Egress       RuntimeEgressPolicy
	HostBinary   string
	HostServices []runtimehost.HostService
}

type RuntimeEgressPolicy struct {
	AllowedHosts  []string
	DefaultAction PolicyAction
}

type HostedApp struct {
	ID         string
	SessionID  string
	AppName    string
	DialTarget string
}

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
	Support(ctx context.Context) (Support, error)
	ListSessions(ctx context.Context, req ListSessionsRequest) (*ListSessionsResponse, error)
	StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
	GetSession(ctx context.Context, req GetSessionRequest) (*Session, error)
	StopSession(ctx context.Context, req StopSessionRequest) error
	StartApp(ctx context.Context, req StartAppRequest) (*HostedApp, error)
	Close() error
}

type WorkspaceProvider interface {
	PrepareWorkspace(ctx context.Context, req PrepareWorkspaceRequest) (*PreparedWorkspace, error)
	RemoveWorkspace(ctx context.Context, req RemoveWorkspaceRequest) error
}

func NormalizeListSessionsRequest(req ListSessionsRequest) (ListSessionsRequest, error) {
	normalized, err := normalizeListSessionsRequest(req)
	if err != nil {
		return ListSessionsRequest{}, err
	}
	return ListSessionsRequest{
		PageSize:  normalized.pageSize,
		PageToken: strings.TrimSpace(req.PageToken),
	}, nil
}

func NormalizeListSessionsRequestForForwarding(req ListSessionsRequest) (ListSessionsRequest, error) {
	pageSize := req.PageSize
	pageToken := strings.TrimSpace(req.PageToken)
	if pageSize < 0 {
		return ListSessionsRequest{}, fmt.Errorf("%w: page_size must be non-negative", ErrInvalidListSessionsPagination)
	}
	if pageSize == 0 && pageToken != "" {
		return ListSessionsRequest{PageToken: pageToken}, nil
	}
	if pageSize == 0 {
		pageSize = DefaultListSessionsPageSize
	}
	if pageSize > MaxListSessionsPageSize {
		pageSize = MaxListSessionsPageSize
	}
	return ListSessionsRequest{
		PageSize:  pageSize,
		PageToken: pageToken,
	}, nil
}

func PaginateSortedSessionIDs(sessionIDs []string, req ListSessionsRequest) ([]string, string, error) {
	normalized, err := normalizeListSessionsRequest(req)
	if err != nil {
		return nil, "", err
	}
	start := 0
	if normalized.lastID != "" {
		start = sort.Search(len(sessionIDs), func(i int) bool {
			return sessionIDs[i] > normalized.lastID
		})
	}
	if start >= len(sessionIDs) {
		return nil, "", nil
	}
	end := start + normalized.pageSize
	if end > len(sessionIDs) {
		end = len(sessionIDs)
	}
	nextPageToken := ""
	if end < len(sessionIDs) {
		nextPageToken, err = encodeListSessionsPageToken(listSessionsPageToken{
			Version:  listSessionsPageTokenVersion,
			PageSize: normalized.pageSize,
			LastID:   sessionIDs[end-1],
		})
		if err != nil {
			return nil, "", err
		}
	}
	return sessionIDs[start:end], nextPageToken, nil
}

func normalizeListSessionsRequest(req ListSessionsRequest) (normalizedListSessionsRequest, error) {
	pageSize := req.PageSize
	if pageSize < 0 {
		return normalizedListSessionsRequest{}, fmt.Errorf("%w: page_size must be non-negative", ErrInvalidListSessionsPagination)
	}
	rawToken := strings.TrimSpace(req.PageToken)
	var token listSessionsPageToken
	if rawToken != "" {
		decoded, err := decodeListSessionsPageToken(rawToken)
		if err != nil {
			return normalizedListSessionsRequest{}, err
		}
		token = decoded
		if token.Version != listSessionsPageTokenVersion {
			return normalizedListSessionsRequest{}, fmt.Errorf("%w: page_token has unsupported version", ErrInvalidListSessionsPagination)
		}
		if token.PageSize <= 0 || token.PageSize > MaxListSessionsPageSize {
			return normalizedListSessionsRequest{}, fmt.Errorf("%w: page_token has invalid page_size", ErrInvalidListSessionsPagination)
		}
	}
	if pageSize == 0 {
		if rawToken != "" {
			pageSize = token.PageSize
		} else {
			pageSize = DefaultListSessionsPageSize
		}
	}
	if pageSize > MaxListSessionsPageSize {
		pageSize = MaxListSessionsPageSize
	}
	if rawToken == "" {
		return normalizedListSessionsRequest{pageSize: pageSize}, nil
	}
	if token.PageSize != pageSize {
		return normalizedListSessionsRequest{}, fmt.Errorf("%w: page_token does not match page_size", ErrInvalidListSessionsPagination)
	}
	return normalizedListSessionsRequest{
		pageSize: pageSize,
		lastID:   token.LastID,
	}, nil
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
