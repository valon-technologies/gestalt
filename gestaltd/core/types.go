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
	UserID            string
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

type APIToken struct {
	ID          string
	UserID      string
	OwnerKind   string
	OwnerID     string
	Name        string
	HashedToken string
	Scopes      string
	Permissions []AccessPermission
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const (
	APITokenOwnerKindUser            = "user"
	APITokenOwnerKindManagedIdentity = "managed_identity"
)

type AccessPermission struct {
	Plugin     string   `json:"plugin"`
	Operations []string `json:"operations,omitempty"`
}
type ManagedIdentity struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ManagedIdentityMembership struct {
	ID         string
	IdentityID string
	UserID     string
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

const (
	PrincipalKindUser           = "user"
	PrincipalKindServiceAccount = "service_account"
)

type Principal struct {
	ID          string
	Kind        string
	Status      string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UserProfile struct {
	PrincipalID     string
	Email           string
	NormalizedEmail string
	AuthProvider    string
	AuthSubject     string
	AvatarURL       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ServiceAccount struct {
	PrincipalID          string
	Name                 string
	Description          string
	CreatedByPrincipalID string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

const (
	ServiceAccountManagementRoleViewer = "viewer"
	ServiceAccountManagementRoleEditor = "editor"
	ServiceAccountManagementRoleAdmin  = "admin"
)

type ServiceAccountManagementGrant struct {
	ID                              string
	MemberPrincipalID               string
	TargetServiceAccountPrincipalID string
	Role                            string
	ExpiresAt                       *time.Time
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
}

const (
	WorkspaceRoleAdmin    = "admin"
	WorkspaceRoleOperator = "operator"
)

type WorkspaceRole struct {
	ID          string
	PrincipalID string
	Role        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PrincipalPluginAccess struct {
	ID                  string
	PrincipalID         string
	Plugin              string
	InvokeAllOperations bool
	Operations          []string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type ServiceAccountDelegation struct {
	ID                              string
	ActorUserPrincipalID            string
	TargetServiceAccountPrincipalID string
	Plugin                          string
	ExpiresAt                       *time.Time
	CreatedAt                       time.Time
	UpdatedAt                       time.Time
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

const (
	APITokenKindAPI      = "api"
	APITokenKindWorkload = "workload"
)

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
	PrincipalID       string
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

type ServiceAccountAuthBinding struct {
	ID                        string
	ServiceAccountPrincipalID string
	BindingKind               string
	LookupKey                 string
	BindingJSON               string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
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
