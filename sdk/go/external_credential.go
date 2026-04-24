package gestalt

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

type ExternalCredential = proto.ExternalCredential
type ExternalCredentialLookup = proto.ExternalCredentialLookup
type UpsertExternalCredentialRequest = proto.UpsertExternalCredentialRequest
type GetExternalCredentialRequest = proto.GetExternalCredentialRequest
type ListExternalCredentialsRequest = proto.ListExternalCredentialsRequest
type ListExternalCredentialsResponse = proto.ListExternalCredentialsResponse
type DeleteExternalCredentialRequest = proto.DeleteExternalCredentialRequest

// ExternalCredentialProvider serves CRUD operations for host-managed external
// credentials.
type ExternalCredentialProvider interface {
	PluginProvider
	UpsertCredential(ctx context.Context, req *UpsertExternalCredentialRequest) (*ExternalCredential, error)
	GetCredential(ctx context.Context, req *GetExternalCredentialRequest) (*ExternalCredential, error)
	ListCredentials(ctx context.Context, req *ListExternalCredentialsRequest) (*ListExternalCredentialsResponse, error)
	DeleteCredential(ctx context.Context, req *DeleteExternalCredentialRequest) error
}
