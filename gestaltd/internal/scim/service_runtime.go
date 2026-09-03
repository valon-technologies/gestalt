package scim

import "context"

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
