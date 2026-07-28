package indexeddb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/hostserviceingress"
)

// RemoteDevelopmentStoreNameResolver maps logical object-store names through an
// upstream-created, session-scoped namespace. It is used for locally-developed
// apps that are reverse-published into production.
type RemoteDevelopmentStoreNameResolver struct {
	Namespaces   *coredata.RemoteIndexedDBNamespaceService
	AppName      string
	ProviderName string
	DatabaseName string
}

func (r *RemoteDevelopmentStoreNameResolver) ResolveStoreName(ctx context.Context, logicalName string) (string, *ResolvedStoreScope, error) {
	if r == nil || r.Namespaces == nil {
		return logicalName, nil, nil
	}
	claims, ok := hostserviceingress.IndexedDBNamespaceFromContext(ctx)
	if !ok {
		// No verified namespace claim; fall back to the logical name unchanged.
		return logicalName, nil, nil
	}
	if strings.TrimSpace(claims.ProviderName) != r.ProviderName || strings.TrimSpace(claims.DatabaseName) != r.DatabaseName {
		return "", nil, status.Error(codes.PermissionDenied, "indexeddb namespace binding mismatch")
	}
	if strings.TrimSpace(claims.AppName) != r.AppName {
		return "", nil, status.Error(codes.PermissionDenied, "indexeddb namespace app mismatch")
	}
	namespace, err := r.Namespaces.ResolveActive(ctx, claims.NamespaceID, claims.RegistrationID, claims.SessionID, r.AppName, claims.Generation)
	if err != nil {
		return "", nil, mapNamespaceError(err)
	}
	physicalName, err := r.Namespaces.ResolvePhysicalName(ctx, namespace.ID, logicalName)
	if err != nil {
		return "", nil, mapNamespaceError(err)
	}
	return physicalName, &ResolvedStoreScope{
		NamespaceID:  namespace.ID,
		LogicalName:  logicalName,
		PhysicalName: physicalName,
	}, nil
}

// RemoteDevelopmentNamespaceTracker records store mappings in the shared
// namespace state so that asynchronous cleanup can remove physical stores.
type RemoteDevelopmentNamespaceTracker struct {
	Namespaces *coredata.RemoteIndexedDBNamespaceService
}

func (t *RemoteDevelopmentNamespaceTracker) TrackStore(ctx context.Context, scope ResolvedStoreScope) error {
	if t == nil || t.Namespaces == nil {
		return nil
	}
	return t.Namespaces.TrackStore(ctx, scope.NamespaceID, scope.LogicalName, scope.PhysicalName)
}

func (t *RemoteDevelopmentNamespaceTracker) MarkStoreDeleted(ctx context.Context, scope ResolvedStoreScope) error {
	if t == nil || t.Namespaces == nil {
		return nil
	}
	return t.Namespaces.MarkStoreDeleted(ctx, scope.NamespaceID, scope.LogicalName)
}

func mapNamespaceError(err error) error {
	if errors.Is(err, coredata.ErrNotRegistered) {
		return status.Error(codes.PermissionDenied, "indexeddb namespace is not active or not found")
	}
	if errors.Is(err, coredata.ErrGenerationMismatch) {
		return status.Error(codes.PermissionDenied, "indexeddb namespace generation mismatch")
	}
	return status.Error(codes.Unavailable, fmt.Sprintf("indexeddb namespace resolution failed: %v", err))
}
