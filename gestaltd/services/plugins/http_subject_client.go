package plugins

import (
	"context"
	"strings"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ core.HTTPSubjectResolver = (*remoteProviderBase)(nil)

func (p *remoteProviderBase) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	if p == nil || p.client == nil || req == nil {
		return nil, nil
	}

	reqCtx, err := requestContextProto(ctx)
	if err != nil {
		return nil, err
	}

	httpReq, err := httpSubjectRequestProto(req)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.ResolveHTTPSubject(ctx, &proto.ResolveHTTPSubjectRequest{
		Request: httpReq,
		Context: reqCtx,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
			return nil, nil
		}
		return nil, err
	}
	if resp.GetRejectStatus() > 0 {
		return nil, &core.HTTPSubjectResolveError{
			Status:  int(resp.GetRejectStatus()),
			Message: resp.GetRejectMessage(),
		}
	}

	subject := resp.GetSubject()
	if strings.TrimSpace(subject.GetId()) == "" {
		return nil, nil
	}
	return &core.HTTPResolvedSubject{
		ID:          strings.TrimSpace(subject.GetId()),
		Kind:        strings.TrimSpace(subject.GetKind()),
		DisplayName: strings.TrimSpace(subject.GetDisplayName()),
		AuthSource:  strings.TrimSpace(subject.GetAuthSource()),
	}, nil
}

func httpSubjectRequestProto(req *core.HTTPSubjectResolveRequest) (*proto.HTTPSubjectRequest, error) {
	if req == nil {
		return nil, nil
	}

	params, err := structFromMap(req.Params)
	if err != nil {
		return nil, err
	}

	return &proto.HTTPSubjectRequest{
		Binding:         req.Binding,
		Method:          req.Method,
		Path:            req.Path,
		ContentType:     req.ContentType,
		Headers:         mapStringSlices(req.Headers),
		Query:           mapStringSlices(req.Query),
		Params:          params,
		RawBody:         append([]byte(nil), req.RawBody...),
		SecurityScheme:  req.SecurityScheme,
		VerifiedSubject: req.VerifiedSubject,
		VerifiedClaims:  cloneStringMap(req.VerifiedClaims),
	}, nil
}

func mapStringSlices[V ~map[string][]string](values V) map[string]*proto.StringList {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]*proto.StringList, len(values))
	for key, item := range values {
		copied := make([]string, len(item))
		copy(copied, item)
		out[key] = &proto.StringList{Values: copied}
	}
	return out
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
