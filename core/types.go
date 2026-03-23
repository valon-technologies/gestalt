package core

import "time"

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
	Instance          string
	AccessToken       string
	RefreshToken      string
	Scopes            string
	ExpiresAt         *time.Time
	LastRefreshedAt   time.Time
	RefreshErrorCount int
	MetadataJSON      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type APIToken struct {
	ID          string
	UserID      string
	Name        string
	HashedToken string
	Scopes      string
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
	Status int
	Body   string
}
