package core

import "context"

// ExternalCredentialProvider manages subject-scoped third-party credentials
// used to invoke integrations on behalf of users, workloads, or system
// subjects.
type ExternalCredentialProvider interface {
	PutCredential(ctx context.Context, credential *ExternalCredential) error
	RestoreCredential(ctx context.Context, credential *ExternalCredential) error
	GetCredential(ctx context.Context, subjectID, integration, connection, instance string) (*ExternalCredential, error)
	ListCredentials(ctx context.Context, subjectID string) ([]*ExternalCredential, error)
	ListCredentialsForProvider(ctx context.Context, subjectID, integration string) ([]*ExternalCredential, error)
	ListCredentialsForConnection(ctx context.Context, subjectID, integration, connection string) ([]*ExternalCredential, error)
	DeleteCredential(ctx context.Context, id string) error
}
