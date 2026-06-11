package server

import (
	"context"
	"fmt"

	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/providergateway"
)

func (s *Server) withProviderGatewayCallerToken(ctx context.Context, p *principal.Principal) (context.Context, error) {
	subjectID := invokingPrincipalSubjectID(p)
	if subjectID == "" || len(s.sessionIssuer) == 0 {
		return ctx, nil
	}
	claims, err := providergateway.GenerateCallerTokenClaims(subjectID, s.now())
	if err != nil {
		return ctx, fmt.Errorf("provider gateway caller token claims: %w", err)
	}
	token, err := providergateway.Issue(claims, s.sessionIssuer)
	if err != nil {
		return ctx, fmt.Errorf("provider gateway caller token: %w", err)
	}
	return providergateway.WithCallerToken(ctx, token), nil
}
