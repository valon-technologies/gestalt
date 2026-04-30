package externalcredentials

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/egress"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ExecConfig struct {
	Command      string
	Args         []string
	Env          map[string]string
	Config       map[string]any
	Egress       egress.Policy
	HostBinary   string
	Cleanup      func()
	HostServices []runtimehost.HostService
	Name         string
}

type remoteExternalCredentialProvider struct {
	client proto.ExternalCredentialProviderClient
	closer io.Closer
}

func NewExecutable(ctx context.Context, cfg ExecConfig) (core.ExternalCredentialProvider, error) {
	proc, err := runtimehost.StartPluginProcess(ctx, runtimehost.ProcessConfig{
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		Egress:       cfg.Egress,
		HostBinary:   cfg.HostBinary,
		Cleanup:      cfg.Cleanup,
		HostServices: cfg.HostServices,
		ProviderName: cfg.Name,
	})
	if err != nil {
		return nil, err
	}

	runtimeClient := proc.Lifecycle()
	client := proto.NewExternalCredentialProviderClient(proc.Conn())
	if _, err := runtimehost.ConfigureRuntimeProvider(ctx, runtimeClient, proto.ProviderKind_PROVIDER_KIND_EXTERNAL_CREDENTIAL, cfg.Name, cfg.Config); err != nil {
		_ = proc.Close()
		return nil, err
	}

	return &remoteExternalCredentialProvider{client: client, closer: proc}, nil
}

func (r *remoteExternalCredentialProvider) PutCredential(ctx context.Context, credential *core.ExternalCredential) error {
	value, err := r.upsertCredential(ctx, credential, false)
	if err != nil {
		return err
	}
	*credential = *value
	return nil
}

func (r *remoteExternalCredentialProvider) RestoreCredential(ctx context.Context, credential *core.ExternalCredential) error {
	value, err := r.upsertCredential(ctx, credential, true)
	if err != nil {
		return err
	}
	*credential = *value
	return nil
}

func (r *remoteExternalCredentialProvider) GetCredential(ctx context.Context, subjectID, integration, connection, instance string) (*core.ExternalCredential, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := r.client.GetCredential(ctx, &proto.GetExternalCredentialRequest{
		Lookup: &proto.ExternalCredentialLookup{
			SubjectId:   strings.TrimSpace(subjectID),
			Integration: strings.TrimSpace(integration),
			Connection:  strings.TrimSpace(connection),
			Instance:    strings.TrimSpace(instance),
		},
	})
	if err != nil {
		return nil, externalCredentialRPCError("get external credential", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("get external credential: provider returned nil credential")
	}
	return externalCredentialFromProto(resp), nil
}

func (r *remoteExternalCredentialProvider) ListCredentials(ctx context.Context, subjectID string) ([]*core.ExternalCredential, error) {
	return r.listCredentials(ctx, &proto.ListExternalCredentialsRequest{
		SubjectId: strings.TrimSpace(subjectID),
	})
}

func (r *remoteExternalCredentialProvider) ListCredentialsForProvider(ctx context.Context, subjectID, integration string) ([]*core.ExternalCredential, error) {
	return r.listCredentials(ctx, &proto.ListExternalCredentialsRequest{
		SubjectId:   strings.TrimSpace(subjectID),
		Integration: strings.TrimSpace(integration),
	})
}

func (r *remoteExternalCredentialProvider) ListCredentialsForConnection(ctx context.Context, subjectID, integration, connection string) ([]*core.ExternalCredential, error) {
	return r.listCredentials(ctx, &proto.ListExternalCredentialsRequest{
		SubjectId:   strings.TrimSpace(subjectID),
		Integration: strings.TrimSpace(integration),
		Connection:  strings.TrimSpace(connection),
	})
}

func (r *remoteExternalCredentialProvider) DeleteCredential(ctx context.Context, id string) error {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	_, err := r.client.DeleteCredential(ctx, &proto.DeleteExternalCredentialRequest{
		Id: strings.TrimSpace(id),
	})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete external credential: %w", err)
	}
	return nil
}

func (r *remoteExternalCredentialProvider) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

func (r *remoteExternalCredentialProvider) upsertCredential(ctx context.Context, credential *core.ExternalCredential, preserveTimestamps bool) (*core.ExternalCredential, error) {
	if credential == nil {
		return nil, fmt.Errorf("external credential is required")
	}
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := r.client.UpsertCredential(ctx, &proto.UpsertExternalCredentialRequest{
		Credential:         externalCredentialToProto(credential),
		PreserveTimestamps: preserveTimestamps,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert external credential: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("upsert external credential: provider returned nil credential")
	}
	return externalCredentialFromProto(resp), nil
}

func (r *remoteExternalCredentialProvider) listCredentials(ctx context.Context, req *proto.ListExternalCredentialsRequest) ([]*core.ExternalCredential, error) {
	ctx, cancel := providerCallContext(ctx)
	defer cancel()

	resp, err := r.client.ListCredentials(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list external credentials: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("list external credentials: provider returned nil response")
	}
	out := make([]*core.ExternalCredential, 0, len(resp.GetCredentials()))
	for _, credential := range resp.GetCredentials() {
		out = append(out, externalCredentialFromProto(credential))
	}
	return out, nil
}

func externalCredentialToProto(credential *core.ExternalCredential) *proto.ExternalCredential {
	if credential == nil {
		return nil
	}
	return &proto.ExternalCredential{
		Id:                credential.ID,
		SubjectId:         strings.TrimSpace(credential.SubjectID),
		Integration:       strings.TrimSpace(credential.Integration),
		Connection:        strings.TrimSpace(credential.Connection),
		Instance:          strings.TrimSpace(credential.Instance),
		AccessToken:       credential.AccessToken,
		RefreshToken:      credential.RefreshToken,
		Scopes:            credential.Scopes,
		ExpiresAt:         timeToProto(credential.ExpiresAt),
		LastRefreshedAt:   timeToProto(credential.LastRefreshedAt),
		RefreshErrorCount: int32(credential.RefreshErrorCount),
		MetadataJson:      credential.MetadataJSON,
		CreatedAt:         timeToProto(nonZeroTimePtr(credential.CreatedAt)),
		UpdatedAt:         timeToProto(nonZeroTimePtr(credential.UpdatedAt)),
	}
}

func externalCredentialFromProto(credential *proto.ExternalCredential) *core.ExternalCredential {
	if credential == nil {
		return nil
	}
	return &core.ExternalCredential{
		ID:                strings.TrimSpace(credential.GetId()),
		SubjectID:         strings.TrimSpace(credential.GetSubjectId()),
		Integration:       strings.TrimSpace(credential.GetIntegration()),
		Connection:        strings.TrimSpace(credential.GetConnection()),
		Instance:          strings.TrimSpace(credential.GetInstance()),
		AccessToken:       credential.GetAccessToken(),
		RefreshToken:      credential.GetRefreshToken(),
		Scopes:            credential.GetScopes(),
		ExpiresAt:         timeFromProto(credential.GetExpiresAt()),
		LastRefreshedAt:   timeFromProto(credential.GetLastRefreshedAt()),
		RefreshErrorCount: int(credential.GetRefreshErrorCount()),
		MetadataJSON:      credential.GetMetadataJson(),
		CreatedAt:         derefTime(timeFromProto(credential.GetCreatedAt())),
		UpdatedAt:         derefTime(timeFromProto(credential.GetUpdatedAt())),
	}
}

func externalCredentialRPCError(operation string, err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return core.ErrNotFound
	case codes.OK:
		return nil
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func nonZeroTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func timeToProto(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func timeFromProto(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	value := t.AsTime()
	return &value
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
