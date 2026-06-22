package providergateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type AuthorizationParams struct {
	ProviderID  string
	Operation   string
	CallerToken string
}

func (t *ProviderGatewayTransport) Authorize(ctx context.Context, params AuthorizationParams) (bool, error) {
	if t == nil || t.authorization == nil {
		return true, nil
	}
	allowed, req := t.shadowAuthorizationCheck(ctx, params)
	recordProviderGatewayAuthorizationCheck(ctx, allowed, strings.TrimSpace(params.CallerToken) != "", req)
	return true, nil
}

func (t *ProviderGatewayTransport) shadowAuthorizationCheck(ctx context.Context, params AuthorizationParams) (bool, *proto.CheckAccessRequest) {
	req, err := t.checkAccessRequest(params)
	if err != nil {
		return false, nil
	}
	resp, err := t.authorization.CheckAccess(ctx, req)
	if err != nil || resp == nil {
		return false, req
	}
	return resp.GetAllowed(), req
}

type DirectTransport struct{}

func (DirectTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (resp ProviderGatewayResponse, err error) {
	startedAt := time.Now()
	defer func() {
		recordProviderGatewayOperation(ctx, startedAt, err, req, TransportPathDirect)
	}()

	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	return next(ctx, req)
}

type ProviderGatewayTransport struct {
	authorization        AuthorizationProvider
	callerTokenPublicKey string
}

func NewProviderGatewayTransport() *ProviderGatewayTransport {
	return &ProviderGatewayTransport{}
}

func (t *ProviderGatewayTransport) SetAuthorizationProvider(authorization AuthorizationProvider) {
	if t == nil {
		return
	}
	t.authorization = authorization
}

func (t *ProviderGatewayTransport) SetCallerTokenPublicKey(publicKey string) {
	if t == nil {
		return
	}
	t.callerTokenPublicKey = strings.TrimSpace(publicKey)
}

func (t *ProviderGatewayTransport) Invoke(ctx context.Context, req ProviderGatewayRequest, next Next) (resp ProviderGatewayResponse, err error) {
	startedAt := time.Now()
	defer func() {
		recordProviderGatewayOperation(ctx, startedAt, err, req, TransportPathProviderGateway)
	}()

	if t == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: transport is nil")
	}
	if next == nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: next handler is required")
	}
	allowed, err := t.Authorize(ctx, AuthorizationParams{
		ProviderID:  req.ProviderID,
		Operation:   req.Operation,
		CallerToken: req.CallerToken,
	})
	if err != nil {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: authorize: %w", err)
	}
	if !allowed {
		return ProviderGatewayResponse{}, fmt.Errorf("provider gateway: unauthorized")
	}
	return next(ctx, req)
}

func (t *ProviderGatewayTransport) checkAccessRequest(params AuthorizationParams) (*proto.CheckAccessRequest, error) {
	subject, err := t.authorizationSubject(params.CallerToken)
	if err != nil {
		return nil, err
	}
	resource, err := authorizationResource(params.ProviderID)
	if err != nil {
		return nil, err
	}
	action, err := authorizationAction(params.Operation)
	if err != nil {
		return nil, err
	}
	return &proto.CheckAccessRequest{
		Subject:  subject,
		Resource: resource,
		Action:   action,
	}, nil
}

func (t *ProviderGatewayTransport) authorizationSubject(callerToken string) (*proto.Subject, error) {
	if t == nil || strings.TrimSpace(t.callerTokenPublicKey) == "" {
		return nil, fmt.Errorf("provider gateway: caller token public key is required")
	}
	subjectID, err := CallerTokenSubjectID(callerToken, t.callerTokenPublicKey)
	if err != nil {
		return nil, fmt.Errorf("provider gateway: caller token subject: %w", err)
	}
	return &proto.Subject{
		Type: "subject",
		Id:   subjectID,
	}, nil
}

func authorizationResource(providerID string) (*proto.Resource, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, fmt.Errorf("provider gateway: provider id is required")
	}
	return &proto.Resource{
		Type: "provider",
		Id:   providerID,
	}, nil
}

func authorizationAction(operation string) (*proto.Action, error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return nil, fmt.Errorf("provider gateway: operation is required")
	}
	return &proto.Action{Name: operation}, nil
}
