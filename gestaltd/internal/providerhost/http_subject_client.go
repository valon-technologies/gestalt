package providerhost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
)

var _ core.HTTPSubjectResolver = (*remoteProviderBase)(nil)

func (p *remoteProviderBase) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	if p == nil || p.client == nil || req == nil {
		return nil, nil
	}

	params, err := structFromMap(map[string]any{
		"binding":          req.Binding,
		"method":           req.Method,
		"path":             req.Path,
		"content_type":     req.ContentType,
		"headers":          mapStringSlices(req.Headers),
		"query":            mapStringSlices(req.Query),
		"params":           req.Params,
		"raw_body_base64":  base64.StdEncoding.EncodeToString(req.RawBody),
		"security_scheme":  req.SecurityScheme,
		"verified_subject": req.VerifiedSubject,
		"verified_claims":  req.VerifiedClaims,
	})
	if err != nil {
		return nil, err
	}

	reqCtx, err := requestContextProto(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Execute(ctx, &proto.ExecuteRequest{
		Operation: proto.InternalResolveHTTPSubjectOperation,
		Params:    params,
		Context:   reqCtx,
	})
	if err != nil {
		return nil, err
	}

	switch status := int(resp.GetStatus()); {
	case status == http.StatusNotFound && isUnknownOperationBody(resp.GetBody()):
		return nil, nil
	case status == http.StatusNoContent:
		return nil, nil
	case status != http.StatusOK:
		return nil, &core.HTTPSubjectResolveError{
			Status:  status,
			Message: operationErrorMessage(resp.GetBody()),
		}
	}

	var subject core.HTTPResolvedSubject
	if err := json.Unmarshal([]byte(resp.GetBody()), &subject); err != nil {
		return nil, fmt.Errorf("decode resolved http subject: %w", err)
	}
	if subject.ID == "" {
		return nil, nil
	}
	return &subject, nil
}

func mapStringSlices[V ~map[string][]string](values V) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, item := range values {
		copied := make([]string, len(item))
		copy(copied, item)
		out[key] = copied
	}
	return out
}

func isUnknownOperationBody(body string) bool {
	var payload map[string]string
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	return payload["error"] == "unknown operation"
}

func operationErrorMessage(body string) string {
	var payload map[string]string
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return body
	}
	if msg := payload["error"]; msg != "" {
		return msg
	}
	return body
}
