package scim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	UserSchemaURN                  = "urn:ietf:params:scim:schemas:core:2.0:User"
	GroupSchemaURN                 = "urn:ietf:params:scim:schemas:core:2.0:Group"
	PatchSchemaURN                 = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	ListSchemaURN                  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	ErrorSchemaURN                 = "urn:ietf:params:scim:api:messages:2.0:Error"
	ServiceProviderConfigSchemaURN = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	ResourceTypeSchemaURN          = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
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
	Schemas     []string   `json:"schemas"`
	ID          string     `json:"id"`
	ExternalID  string     `json:"externalId,omitempty"`
	UserName    string     `json:"userName"`
	Active      bool       `json:"active"`
	DisplayName string     `json:"displayName,omitempty"`
	Name        Name       `json:"name,omitempty"`
	Emails      []Email    `json:"emails,omitempty"`
	Groups      []GroupRef `json:"groups,omitempty"`
	Meta        Meta       `json:"meta"`
}

// Name is a value struct for convenient callers, but SCIM omits the optional
// complex attribute when every component is unset.
func (u User) MarshalJSON() ([]byte, error) {
	type alias User
	b, err := json.Marshal(alias(u))
	if err != nil {
		return nil, err
	}
	if u.Name != (Name{}) {
		return b, nil
	}
	var object map[string]any
	if err := json.Unmarshal(b, &object); err != nil {
		return nil, err
	}
	delete(object, "name")
	return json.Marshal(object)
}

// GroupRef is the read-only User.groups representation defined by RFC 7643.
type GroupRef struct {
	Value   string `json:"value"`
	Ref     string `json:"$ref,omitempty"`
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
}

type Member struct {
	Value   string `json:"value"`
	Ref     string `json:"$ref,omitempty"`
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
}

type Group struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []Member `json:"members,omitempty"`
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

type groupInput struct {
	Schemas     []string  `json:"schemas"`
	ExternalID  *string   `json:"externalId,omitempty"`
	DisplayName *string   `json:"displayName,omitempty"`
	Members     *[]Member `json:"members,omitempty"`
}

type persistedUser struct {
	ExternalID  string  `json:"externalId,omitempty"`
	UserName    string  `json:"userName"`
	Active      bool    `json:"active"`
	DisplayName string  `json:"displayName,omitempty"`
	Name        Name    `json:"name,omitempty"`
	Emails      []Email `json:"emails,omitempty"`
}

type persistedGroup struct {
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []Member `json:"members,omitempty"`
}

type listResponse[T any] struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []T      `json:"Resources"`
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

func invalidSyntax(detail string) *Error {
	return &Error{Status: http.StatusBadRequest, SCIMType: "invalidSyntax", Detail: detail}
}

func mutability(detail string) *Error {
	return &Error{Status: http.StatusBadRequest, SCIMType: "mutability", Detail: detail}
}

func invalidFilter(detail string) *Error {
	return &Error{Status: http.StatusBadRequest, SCIMType: "invalidFilter", Detail: detail}
}

func noTarget(detail string) *Error {
	return &Error{Status: http.StatusBadRequest, SCIMType: "noTarget", Detail: detail}
}

func invalidPath(detail string) *Error {
	return &Error{Status: http.StatusBadRequest, SCIMType: "invalidPath", Detail: detail}
}

func notFound() *Error {
	return &Error{Status: http.StatusNotFound, Detail: "SCIM User was not found"}
}

func notFoundResource(resource string) *Error {
	return &Error{Status: http.StatusNotFound, Detail: resource + " was not found"}
}

func conflict(detail string) *Error {
	return &Error{Status: http.StatusConflict, SCIMType: "uniqueness", Detail: detail}
}

func unavailable(detail string) *Error {
	return &Error{Status: http.StatusServiceUnavailable, Detail: detail, Retry: true}
}

func contentETag(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return `W/"` + hex.EncodeToString(sum[:]) + `"`
}
func etag(value any) string { return contentETag(value) }

func normalize(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

// validateResourceSchemas enforces the resource's base schema while allowing
// vendor extension URNs that Gestalt does not persist. The base schema must
// occur exactly once; another core resource schema is never an extension.
func validateResourceSchemas(schemas []string, base string) error {
	if len(schemas) == 0 {
		return invalidSyntax("schemas must contain the resource base schema")
	}
	seen := make(map[string]struct{}, len(schemas))
	found := false
	for _, raw := range schemas {
		schema := strings.TrimSpace(raw)
		if schema == "" {
			return invalidSyntax("schemas must not contain empty values")
		}
		if _, ok := seen[schema]; ok {
			return invalidSyntax("schemas must not contain duplicate values")
		}
		seen[schema] = struct{}{}
		switch schema {
		case base:
			found = true
		case UserSchemaURN, GroupSchemaURN, PatchSchemaURN, ListSchemaURN, ErrorSchemaURN:
			return invalidSyntax("schemas contains an unsupported resource schema")
		default:
			if !strings.HasPrefix(strings.ToLower(schema), "urn:") {
				return invalidSyntax("schemas contains an unsupported value")
			}
		}
	}
	if !found {
		return invalidSyntax("schemas must contain the resource base schema")
	}
	return nil
}

func validatePatchSchemas(schemas []string) error {
	if len(schemas) == 0 {
		return invalidSyntax("PATCH schemas must contain the PatchOp schema")
	}
	seen := map[string]struct{}{}
	found := false
	for _, raw := range schemas {
		schema := strings.TrimSpace(raw)
		if schema == "" {
			return invalidSyntax("PATCH schemas must not contain empty values")
		}
		if _, ok := seen[schema]; ok {
			return invalidSyntax("PATCH schemas must not contain duplicate values")
		}
		seen[schema] = struct{}{}
		if schema == PatchSchemaURN {
			found = true
		} else if schema == UserSchemaURN || schema == GroupSchemaURN || schema == ListSchemaURN || schema == ErrorSchemaURN || !strings.HasPrefix(strings.ToLower(schema), "urn:") {
			return invalidSyntax("PATCH schemas contains an unsupported value")
		}
	}
	if !found {
		return invalidSyntax("PATCH schemas must contain the PatchOp schema")
	}
	return nil
}

func validateEmails(emails []Email) error {
	primary := 0
	for _, email := range emails {
		if strings.TrimSpace(email.Value) == "" {
			return invalid("email value is required")
		}
		if email.Primary {
			primary++
		}
	}
	if primary > 1 {
		return invalid("emails may contain at most one primary value")
	}
	return nil
}
