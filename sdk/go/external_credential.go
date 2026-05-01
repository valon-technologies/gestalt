package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/internal/gen/v1"
)

// ExternalCredential is the generated credential record stored by the host.
type ExternalCredential = proto.ExternalCredential

// ExternalCredentialLookup selects a host-managed external credential.
type ExternalCredentialLookup = proto.ExternalCredentialLookup

// UpsertExternalCredentialRequest is the request for creating or updating a credential.
type UpsertExternalCredentialRequest = proto.UpsertExternalCredentialRequest

// GetExternalCredentialRequest is the request for fetching one credential.
type GetExternalCredentialRequest = proto.GetExternalCredentialRequest

// ListExternalCredentialsRequest is the request for listing credentials.
type ListExternalCredentialsRequest = proto.ListExternalCredentialsRequest

// ListExternalCredentialsResponse is the response returned when listing credentials.
type ListExternalCredentialsResponse = proto.ListExternalCredentialsResponse

// DeleteExternalCredentialRequest is the request for deleting one credential.
type DeleteExternalCredentialRequest = proto.DeleteExternalCredentialRequest

// ExternalCredentialProvider serves CRUD operations for host-managed external
// credentials.
type ExternalCredentialProvider interface {
	Provider
	UpsertCredential(ctx context.Context, req *UpsertExternalCredentialRequest) (*ExternalCredential, error)
	GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error)
	ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error)
	DeleteCredential(ctx context.Context, req *DeleteExternalCredentialRequest) error
}
