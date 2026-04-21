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

type IntegrationToken struct {
	ID                string
	SubjectID         string
	Integration       string
	Connection        string
	Instance          string
	AccessToken       string
	RefreshToken      string
	Scopes            string
	ExpiresAt         *time.Time
	LastRefreshedAt   *time.Time
	RefreshErrorCount int
	MetadataJSON      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type AccessPermission struct {
	Plugin     string   `json:"plugin"`
	Operations []string `json:"operations,omitempty"`
}

type APIToken struct {
	ID                  string
	IdentityID          string
	OwnerKind           string
	OwnerID             string
	CredentialSubjectID string
	TokenKind           string
	Name                string
	HashedToken         string
	Scopes              string
	Permissions         []AccessPermission
	ExpiresAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const (
	APITokenOwnerKindUser            = "user"
	APITokenOwnerKindManagedIdentity = "managed_identity"
)

const (
	APITokenKindAPI      = "api"
	APITokenKindWorkload = "workload"
)

type ManagedIdentity struct {
	ID                  string
	DisplayName         string
	CreatedByIdentityID string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ManagedIdentityMembership struct {
	ID         string
	IdentityID string
	SubjectID  string
	Email      string
	Role       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ManagedIdentityGrant struct {
	ID         string
	IdentityID string
	Plugin     string
	Operations []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Identity struct {
	ID                  string
	Status              string
	DisplayName         string
	CreatedByIdentityID string
	MetadataJSON        string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type IdentityAuthBinding struct {
	ID          string
	IdentityID  string
	BindingKind string
	Authority   string
	LookupKey   string
	BindingJSON string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const (
	IdentityAuthBindingKindOIDCSubject          = "oidc_subject"
	IdentityAuthBindingKindEmail                = "email"
	IdentityAuthBindingKindSPIFFE               = "spiffe"
	IdentityAuthBindingKindKubernetesServiceAcc = "kubernetes_serviceaccount"
)

const (
	IdentityManagementRoleViewer = "viewer"
	IdentityManagementRoleEditor = "editor"
	IdentityManagementRoleAdmin  = "admin"
)

type IdentityManagementGrant struct {
	ID                string
	ManagerIdentityID string
	TargetIdentityID  string
	Role              string
	ExpiresAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const (
	WorkspaceRoleAdmin    = "admin"
	WorkspaceRoleOperator = "operator"
)

type WorkspaceRole struct {
	ID         string
	IdentityID string
	Role       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type IdentityPluginAccess struct {
	ID                  string
	IdentityID          string
	Plugin              string
	InvokeAllOperations bool
	Operations          []string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type IdentityDelegation struct {
	ID               string
	ActorIdentityID  string
	TargetIdentityID string
	Plugin           string
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type APITokenAccess struct {
	ID                  string
	TokenID             string
	Plugin              string
	InvokeAllOperations bool
	Operations          []string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ExternalCredential struct {
	ID                string
	IdentityID        string
	Plugin            string
	Connection        string
	Instance          string
	AuthType          string
	PayloadEncrypted  string
	Scopes            string
	ExpiresAt         *time.Time
	LastRefreshedAt   *time.Time
	RefreshErrorCount int
	MetadataJSON      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type UserIdentity struct {
	Email       string
	DisplayName string
	AvatarURL   string
}

type TokenResponse struct {
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
	Body    string

	// MCPResult, when non-nil, carries the original MCP CallToolResult for
	// passthrough operations so the MCP handler can return it without losing
	// fields like StructuredContent.
	MCPResult any
}
