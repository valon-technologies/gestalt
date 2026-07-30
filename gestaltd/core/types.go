package core

import (
	"context"
	"net/http"
	"time"
)

type User struct {
	ID          string
	Email       string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ManagedSubject struct {
	SubjectID          string
	DisplayName        string
	Description        string
	CreatedBySubjectID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

type AppInstallation struct {
	AppName            string
	Version            string
	SourceRef          string
	Registry           string
	ProviderReleaseURL string
	ArtifactChecksums  map[string]string
	InstalledBy        string
	InstalledAt        time.Time
	UpdatedAt          time.Time
}

type AppVersionChangeRequest struct {
	ID                         string
	App                        string
	FromVersion                string
	ToVersion                  string
	Actor                      string
	Timestamp                  time.Time
	FromVersionDeployableUntil *time.Time
	Metadata                   map[string]any
}

type AppInstanceMaterialization struct {
	InstanceID       string
	SourceVersion    string
	App              string
	Version          string
	AcknowledgedAt   time.Time
	MaterializedAt   time.Time
	StoppedAt        time.Time
	RestartedAt      time.Time
	AttemptCount     int
	LastErrorAt      time.Time
	LastErrorMessage string
}

type GestaltdInstanceAppState string

const (
	GestaltdInstanceAppStateRunning    GestaltdInstanceAppState = "running"
	GestaltdInstanceAppStateStarting   GestaltdInstanceAppState = "starting"
	GestaltdInstanceAppStateNotRunning GestaltdInstanceAppState = "not_running"
	GestaltdInstanceAppStateError      GestaltdInstanceAppState = "error"
	GestaltdInstanceAppStateUnknown    GestaltdInstanceAppState = "unknown"
)

type GestaltdInstanceAppHeartbeat struct {
	State          GestaltdInstanceAppState `json:"state"`
	DesiredVersion string                   `json:"desired_version,omitempty"`
	RunningVersion string                   `json:"running_version,omitempty"`
	ObservedAt     time.Time                `json:"observed_at"`
	LastError      string                   `json:"last_error,omitempty"`
}

type GestaltdInstanceHeartbeat struct {
	InstanceID    string
	SourceVersion string
	StartedAt     time.Time
	HeartbeatAt   time.Time
	Apps          map[string]GestaltdInstanceAppHeartbeat
}

// RegistryAppRuntimeObservation is a coherent, local-only observation of a
// configured registry app. DesiredVersion and ObservedAt are added by the
// heartbeat writer because they come from coredata and the writer's clock.
type RegistryAppRuntimeObservation struct {
	State          GestaltdInstanceAppState
	RunningVersion string
	LastError      string
}

type AppFleetState string

const (
	AppFleetStateHealthy    AppFleetState = "healthy"
	AppFleetStateConverging AppFleetState = "converging"
	AppFleetStateDegraded   AppFleetState = "degraded"
	AppFleetStateUnknown    AppFleetState = "unknown"
)

type AppFleetProjection struct {
	App                     string
	State                   AppFleetState
	SourceVersion           string
	DesiredVersion          string
	MinimumHealthyInstances int
	LiveInstances           int
	RunningDesiredVersion   int
	Mismatched              int
	Errors                  int
	HeartbeatTTL            time.Duration
	EvaluatedAt             time.Time
}

type AppRolloutState string

const (
	AppRolloutStateEnrolling  AppRolloutState = "enrolling"
	AppRolloutStateRestarting AppRolloutState = "restarting"
	AppRolloutStateComplete   AppRolloutState = "complete"
	AppRolloutStateFailed     AppRolloutState = "failed"
)

type AppRolloutMode string

const (
	AppRolloutModeEnrollment AppRolloutMode = "enrollment"
	AppRolloutModeHeartbeat  AppRolloutMode = "heartbeat"
)

type AppRolloutFailureSummary struct {
	LiveInstances           int       `json:"live_instances"`
	MinimumHealthyInstances int       `json:"minimum_healthy_instances"`
	RunningDesiredVersion   int       `json:"running_desired_version"`
	Mismatched              int       `json:"mismatched"`
	Errors                  int       `json:"errors"`
	SourceVersion           string    `json:"source_version"`
	Version                 string    `json:"version"`
	EvaluatedAt             time.Time `json:"evaluated_at"`
}

type AppRollout struct {
	App                     string
	Version                 string
	State                   AppRolloutState
	Mode                    AppRolloutMode
	TargetSourceVersion     string
	MinimumHealthyInstances int
	CreatedAt               time.Time
	EnrollmentEndsAt        time.Time
	Deadline                time.Time
	HealthySince            time.Time
	HeartbeatEvaluatedAt    time.Time
	CompletedAt             time.Time
	FailedAt                time.Time
	FailureSummary          *AppRolloutFailureSummary
}

type AppAutoDeploySettings struct {
	App                 string
	Enabled             bool
	PendingVersion      string
	LastSeenVersion     string
	LastError           string
	LastFailedRolloutAt time.Time
}

type AppVersionRolloutOutcome struct {
	ID          string
	App         string
	Version     string
	CompletedAt time.Time
	FailedAt    time.Time
}

type AppVersionRecoveryObservation struct {
	ID                      string
	App                     string
	Version                 string
	RecoveredAt             time.Time
	SourceVersion           string
	LiveInstances           int
	MinimumHealthyInstances int
}

type GestaltdSourceVersionState struct {
	CurrentSourceVersion    string
	MinimumHealthyInstances int
	UpdatedAt               time.Time
}

type ExternalCredentialGrant struct {
	AccessToken       string
	RefreshToken      string
	Scope             string
	ExpiresAt         *time.Time
	LastRefreshedAt   *time.Time
	RefreshErrorCount int
}

type ExternalCredentialClientInfo struct {
	ClientID              string
	ClientSecret          string
	ClientSecretExpiresAt *time.Time
}

type ExternalCredentialOpaque struct {
	Fields map[string]string
}

type ExternalCredential struct {
	ID        string
	Subject   string
	Audience  string
	Qualifier string

	// Exactly one of Grant, Client, Opaque is set.
	Grant  *ExternalCredentialGrant
	Client *ExternalCredentialClientInfo
	Opaque *ExternalCredentialOpaque

	MetadataJSON string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AccessPermission struct {
	App        string   `json:"app"`
	Operations []string `json:"operations,omitempty"`
}

type UserIdentity struct {
	Email       string
	DisplayName string
	AvatarURL   string
}

type OAuthTokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	TokenType    string
	Extra        map[string]any // all fields from the token endpoint response
}

type Operation struct {
	Name        string
	Description string
	Method      string // HTTP method (GET, POST, PUT, DELETE, PATCH)
	Parameters  []Parameter
}

type Parameter struct {
	Name        string
	Type        string
	Description string
	Required    bool
	Default     any
}

type OperationResult struct {
	Status  int
	Headers http.Header
	Body    []byte

	// MCPResult, when non-nil, carries the original MCP CallToolResult for
	// passthrough operations so the MCP handler can return it without losing
	// fields like StructuredContent.
	MCPResult any
}

// InvokeMetadata is the first frame of a streaming invocation: the HTTP-shaped
// status, headers, and the response media type.
type InvokeMetadata struct {
	Status    int
	Headers   http.Header
	MediaType string
}

// InvokeFrame is one frame in a streaming invocation. The first frame is always
// a Metadata frame; subsequent frames are Data byte chunks. A mid-stream error
// may emit a trailing Metadata frame with a non-2xx status followed by a Data
// frame carrying a JSON error body, after which the stream ends.
type InvokeFrame struct {
	Metadata *InvokeMetadata
	Data     []byte
}

// IsMetadata reports whether this frame is the leading metadata frame.
func (f *InvokeFrame) IsMetadata() bool {
	return f != nil && f.Metadata != nil
}

// StreamReader yields InvokeFrame frames. io.EOF ends the stream.
type StreamReader interface {
	Recv() (*InvokeFrame, error)
}

// StreamReaderFunc is an adapter that lets a plain function implement StreamReader.
type StreamReaderFunc func() (*InvokeFrame, error)

func (f StreamReaderFunc) Recv() (*InvokeFrame, error) { return f() }

// StreamingExecutor is implemented by providers that support streaming
// operation responses. ExecuteStream is only called for operations whose
// catalog response mode is stream.
type StreamingExecutor interface {
	ExecuteStream(ctx context.Context, operation string, params map[string]any, token string) (StreamReader, error)
}
