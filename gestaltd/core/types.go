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

type ExternalCredential struct {
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
	OwnerKind           string
	OwnerID             string
	CredentialSubjectID string
	Name                string
	HashedToken         string
	Scopes              string
	Permissions         []AccessPermission
	ExpiresAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const (
	APITokenOwnerKindUser    = "user"
	APITokenOwnerKindSubject = "subject"
)

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
