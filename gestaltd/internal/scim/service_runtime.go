package scim

import (
	"context"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// Service is the SCIM handle shared by HTTP bootstrap and authorization
// gating. SCIM state is held by CompactService; the AuthorizationProvider is
// the source of truth for activation and membership.
type Service struct{ compact *CompactService }

func (s *Service) Enabled() bool { return s != nil && s.compact != nil && s.compact.Enabled() }

func (s *Service) ClientForToken(token string) (string, bool) {
	if s == nil || s.compact == nil {
		return "", false
	}
	return s.compact.ClientForToken(token)
}

func (s *Service) Start(context.Context) {}

func (s *Service) IsEligible(ctx context.Context, coreID, email string) (bool, error) {
	if s == nil || s.compact == nil {
		return true, nil
	}
	return s.compact.IsEligible(ctx, coreID, email)
}

// ResolveAuthorizationResourceDisplayName returns current metadata from the
// resource-owning SCIM store. Authorization relationships intentionally keep
// only stable resource IDs, so renamed groups do not require relationship
// rewrites to remain readable in admin projections.
func (s *Service) ResolveAuthorizationResourceDisplayName(ctx context.Context, resource *proto.Resource) (string, error) {
	if s == nil || s.compact == nil {
		return "", nil
	}
	return s.compact.authorizationResourceDisplayName(ctx, resource)
}
