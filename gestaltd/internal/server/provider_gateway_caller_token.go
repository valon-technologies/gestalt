package server

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

func (s *Server) withProviderGatewayCallerToken(ctx context.Context, p *principal.Principal) (context.Context, error) {
	subjectID := invokingPrincipalSubjectID(p)
	if subjectID == "" || s.providerGateway == nil {
		return ctx, nil
	}
	token, ok, err := s.providerGateway.IssueCallerToken(subjectID, s.now())
	if err != nil {
		return ctx, fmt.Errorf("provider gateway caller token: %w", err)
	}
	if !ok {
		return ctx, nil
	}
	return providergateway.WithCallerToken(ctx, token), nil
}
