package server

import (
	"context"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

const adminAuthorizationProviderReadPageSize = 500

func (s *Server) readAllAuthorizationRelationships(ctx context.Context, req *core.ListRelationshipsRequest) ([]*core.Relationship, error) {
	if s.authorizationProvider == nil {
		return nil, errAdminAuthorizationUnavailable
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = adminAuthorizationProviderReadPageSize
	}
	pageToken := strings.TrimSpace(req.GetPageToken())
	out := make([]*core.Relationship, 0)
	for {
		resp, err := s.authorizationProvider.ListRelationships(ctx, &core.ListRelationshipsRequest{
			Filter:    req.GetFilter(),
			PageSize:  pageSize,
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, resp.GetRelationships()...)
		pageToken = strings.TrimSpace(resp.GetNextPageToken())
		if pageToken == "" {
			return out, nil
		}
	}
}
