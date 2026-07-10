package core

import (
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

const (
	AppInstallationRolloutStatusPending  = "pending"
	AppInstallationRolloutStatusPromoted = "promoted"
	AppInstallationRolloutStatusFailed   = "failed"
)

const (
	AppInstallationEventTypeInstallRequested   = "install_requested"
	AppInstallationEventTypePromoted           = "promoted"
	AppInstallationEventTypeFailed             = "failed"
	AppInstallationEventTypeRollback           = "rollback"
	AppInstallationEventTypeUninstallRequested = "uninstall_requested"
)

// AppInstallation records the shared fleet-wide rollout for a registry-installed
// app. The IndexedDB primary key id holds the app name (for example g-issues).
type AppInstallation struct {
	AppName                 string
	VersionConstraint       string
	ResolvedVersion         string
	SourceRef               string
	Registry                string
	ProviderReleaseURL      string
	ArtifactChecksums       map[string]string
	RolloutStatus           string
	ActiveSince             *time.Time
	PreviousResolvedVersion string
	InstalledBy             string
	InstalledAt             time.Time
	UpdatedAt               time.Time
}

// AppInstallationEvent is an append-only audit record for install lifecycle
// changes on one app. InstallationID matches app_installations.id.
type AppInstallationEvent struct {
	ID             string
	InstallationID string
	FromVersion    string
	ToVersion      string
	Type           string
	Actor          string
	Timestamp      time.Time
	Metadata       map[string]any
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
