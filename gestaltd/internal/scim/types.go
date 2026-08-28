package scim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	UserSchemaURN  = "urn:ietf:params:scim:schemas:core:2.0:User"
	PatchSchemaURN = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	ListSchemaURN  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	ErrorSchemaURN = "urn:ietf:params:scim:api:messages:2.0:Error"
)

type Name struct {
	Formatted  string `json:"formatted,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
}

type Email struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type Meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location"`
	Version      string    `json:"version"`
}

type User struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	UserName    string   `json:"userName"`
	Active      bool     `json:"active"`
	DisplayName string   `json:"displayName,omitempty"`
	Name        Name     `json:"name,omitempty"`
	Emails      []Email  `json:"emails,omitempty"`
	Meta        Meta     `json:"meta"`
}

type userInput struct {
	Schemas     []string `json:"schemas"`
	ExternalID  *string  `json:"externalId,omitempty"`
	UserName    *string  `json:"userName,omitempty"`
	Active      *bool    `json:"active,omitempty"`
	DisplayName *string  `json:"displayName,omitempty"`
	Name        *Name    `json:"name,omitempty"`
	Emails      *[]Email `json:"emails,omitempty"`
}

type persistedUser struct {
	ExternalID  string  `json:"externalId,omitempty"`
	UserName    string  `json:"userName"`
	Active      bool    `json:"active"`
	DisplayName string  `json:"displayName,omitempty"`
	Name        Name    `json:"name,omitempty"`
	Emails      []Email `json:"emails,omitempty"`
}

type listResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []User   `json:"Resources"`
}

type patchRequest struct {
	Schemas    []string         `json:"schemas"`
	Operations []patchOperation `json:"Operations"`
}

type patchOperation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type Error struct {
	Status   int
	SCIMType string
	Detail   string
	Retry    bool
}

func (e *Error) Error() string { return e.Detail }

func invalid(detail string) *Error {
	return &Error{Status: http.StatusBadRequest, SCIMType: "invalidValue", Detail: detail}
}

func notFound() *Error {
	return &Error{Status: http.StatusNotFound, Detail: "SCIM User was not found"}
}

func conflict(detail string) *Error {
	return &Error{Status: http.StatusConflict, SCIMType: "uniqueness", Detail: detail}
}

func unavailable(detail string) *Error {
	return &Error{Status: http.StatusServiceUnavailable, Detail: detail, Retry: true}
}

func etag(version int64) string { return fmt.Sprintf("W/\"%d\"", version) }

func normalize(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
