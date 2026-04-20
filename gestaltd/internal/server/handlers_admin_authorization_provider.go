package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
)

const (
	adminAuthorizationProviderReadPageSize = 500
	adminAuthorizationDebugDefaultPageSize = 100
	adminAuthorizationDebugMaximumPageSize = 500
)

type adminAuthorizationProviderResponse struct {
	Name          string                         `json:"name"`
	Capabilities  []string                       `json:"capabilities,omitempty"`
	ActiveModelID string                         `json:"activeModelId,omitempty"`
	ActiveModel   *adminAuthorizationModelRecord `json:"activeModel,omitempty"`
}

type adminAuthorizationModelListResponse struct {
	Models        []adminAuthorizationModelRecord `json:"models"`
	NextPageToken string                          `json:"nextPageToken,omitempty"`
}

type adminAuthorizationModelRecord struct {
	ID        string `json:"id"`
	Version   string `json:"version,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type adminAuthorizationRelationshipListResponse struct {
	Relationships []adminAuthorizationRelationshipRecord `json:"relationships"`
	NextPageToken string                                 `json:"nextPageToken,omitempty"`
	ModelID       string                                 `json:"modelId,omitempty"`
}

type adminAuthorizationRelationshipRecord struct {
	Subject  adminAuthorizationRef `json:"subject"`
	Relation string                `json:"relation"`
	Resource adminAuthorizationRef `json:"resource"`
	Managed  bool                  `json:"managed"`
}

type adminAuthorizationRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (s *Server) getAdminAuthorizationProvider(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAdminAuthorizationProviderDebugAvailable(w) {
		return
	}

	metadata, err := s.authorizationProvider.GetMetadata(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read authorization provider metadata")
		return
	}
	active, err := s.authorizationProvider.GetActiveModel(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read active authorization model")
		return
	}

	resp := adminAuthorizationProviderResponse{
		Name: s.authorizationProvider.Name(),
	}
	if metadata != nil {
		resp.Capabilities = append([]string(nil), metadata.GetCapabilities()...)
		resp.ActiveModelID = strings.TrimSpace(metadata.GetActiveModelId())
	}
	if model := active.GetModel(); model != nil {
		record := adminAuthorizationModelFromRef(model)
		resp.ActiveModel = &record
		if resp.ActiveModelID == "" {
			resp.ActiveModelID = resp.ActiveModel.ID
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) listAdminAuthorizationModels(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAdminAuthorizationProviderDebugAvailable(w) {
		return
	}

	pageSize, ok := adminAuthorizationDebugPageSize(w, r)
	if !ok {
		return
	}
	resp, err := s.authorizationProvider.ListModels(r.Context(), &core.ListModelsRequest{
		PageSize:  int32(pageSize),
		PageToken: strings.TrimSpace(r.URL.Query().Get("pageToken")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list authorization models")
		return
	}

	out := adminAuthorizationModelListResponse{
		Models:        make([]adminAuthorizationModelRecord, 0, len(resp.GetModels())),
		NextPageToken: strings.TrimSpace(resp.GetNextPageToken()),
	}
	for _, model := range resp.GetModels() {
		out.Models = append(out.Models, adminAuthorizationModelFromRef(model))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAdminAuthorizationRelationships(w http.ResponseWriter, r *http.Request) {
	if !s.ensureAdminAuthorizationProviderDebugAvailable(w) {
		return
	}

	pageSize, ok := adminAuthorizationDebugPageSize(w, r)
	if !ok {
		return
	}
	req := &core.ReadRelationshipsRequest{
		PageSize:  int32(pageSize),
		PageToken: strings.TrimSpace(r.URL.Query().Get("pageToken")),
		ModelId:   strings.TrimSpace(r.URL.Query().Get("modelId")),
	}
	if subjectType := strings.TrimSpace(r.URL.Query().Get("subjectType")); subjectType != "" || strings.TrimSpace(r.URL.Query().Get("subjectId")) != "" {
		req.Subject = &core.SubjectRef{
			Type: subjectType,
			Id:   strings.TrimSpace(r.URL.Query().Get("subjectId")),
		}
	}
	req.Relation = strings.TrimSpace(r.URL.Query().Get("relation"))
	if resourceType := strings.TrimSpace(r.URL.Query().Get("resourceType")); resourceType != "" || strings.TrimSpace(r.URL.Query().Get("resourceId")) != "" {
		req.Resource = &core.ResourceRef{
			Type: resourceType,
			Id:   strings.TrimSpace(r.URL.Query().Get("resourceId")),
		}
	}

	resp, err := s.authorizationProvider.ReadRelationships(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list authorization relationships")
		return
	}

	out := adminAuthorizationRelationshipListResponse{
		Relationships: make([]adminAuthorizationRelationshipRecord, 0, len(resp.GetRelationships())),
		NextPageToken: strings.TrimSpace(resp.GetNextPageToken()),
		ModelID:       strings.TrimSpace(resp.GetModelId()),
	}
	for _, rel := range resp.GetRelationships() {
		if rel == nil {
			continue
		}
		out.Relationships = append(out.Relationships, adminAuthorizationRelationshipFromProvider(rel))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) providerPluginAuthorizationRows(ctx context.Context, plugin string) ([]adminAuthorizationMemberRow, error) {
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Resource: &core.ResourceRef{
			Type: authorization.ProviderResourceTypePluginDynamic,
			Id:   plugin,
		},
	})
	if err != nil {
		return nil, err
	}
	return s.adminAuthorizationRowsFromProviderRelationships(ctx, plugin, relationships)
}

func (s *Server) providerAdminAuthorizationRows(ctx context.Context) ([]adminAuthorizationMemberRow, error) {
	relationships, err := s.readAllAuthorizationRelationships(ctx, &core.ReadRelationshipsRequest{
		PageSize: adminAuthorizationProviderReadPageSize,
		Resource: &core.ResourceRef{
			Type: authorization.ProviderResourceTypeAdminDynamic,
			Id:   authorization.ProviderResourceIDAdminDynamicGlobal,
		},
	})
	if err != nil {
		return nil, err
	}
	return s.adminAuthorizationRowsFromProviderRelationships(ctx, "", relationships)
}

func (s *Server) adminAuthorizationRowsFromProviderRelationships(ctx context.Context, plugin string, relationships []*core.Relationship) ([]adminAuthorizationMemberRow, error) {
	rows := make([]adminAuthorizationMemberRow, 0, len(relationships))
	indexByKey := make(map[string]int, len(relationships))
	for _, rel := range relationships {
		row, ok, err := s.adminAuthorizationDynamicRowFromProviderRelationship(ctx, plugin, rel)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		key := adminAuthorizationDynamicRowDedupeKey(row)
		if idx, exists := indexByKey[key]; exists {
			rows[idx] = row
			continue
		}
		indexByKey[key] = len(rows)
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *Server) adminAuthorizationDynamicRowFromProviderRelationship(ctx context.Context, plugin string, rel *core.Relationship) (adminAuthorizationMemberRow, bool, error) {
	if rel == nil || rel.GetSubject() == nil {
		return adminAuthorizationMemberRow{}, false, nil
	}

	row := adminAuthorizationMemberRow{
		Plugin:    plugin,
		Role:      strings.TrimSpace(rel.GetRelation()),
		Source:    "dynamic",
		Effective: true,
		Mutable:   true,
	}
	if row.Role == "" {
		return adminAuthorizationMemberRow{}, false, nil
	}

	switch strings.TrimSpace(rel.GetSubject().GetType()) {
	case authorization.ProviderSubjectTypeUser:
		userID := strings.TrimSpace(rel.GetSubject().GetId())
		if userID == "" {
			return adminAuthorizationMemberRow{}, false, nil
		}
		row.SelectorKind = "subject_id"
		row.SelectorValue = adminAuthorizationUserSubjectID(userID)
	default:
		return adminAuthorizationMemberRow{}, false, nil
	}

	return row, true, nil
}

func (s *Server) readAllAuthorizationRelationships(ctx context.Context, req *core.ReadRelationshipsRequest) ([]*core.Relationship, error) {
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
		resp, err := s.authorizationProvider.ReadRelationships(ctx, &core.ReadRelationshipsRequest{
			Subject:   req.GetSubject(),
			Relation:  req.GetRelation(),
			Resource:  req.GetResource(),
			PageSize:  pageSize,
			PageToken: pageToken,
			ModelId:   req.GetModelId(),
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

func (s *Server) ensureAdminAuthorizationProviderAvailable(w http.ResponseWriter) bool {
	if s.authorizationProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "authorization provider is unavailable")
		return false
	}
	return true
}

func (s *Server) ensureAdminAuthorizationProviderDebugAvailable(w http.ResponseWriter) bool {
	if !s.ensureAdminAuthorizationProviderAvailable(w) {
		return false
	}
	if strings.TrimSpace(s.adminRoute.AuthorizationPolicy) == "" {
		writeError(w, http.StatusServiceUnavailable, "authorization provider debug is unavailable")
		return false
	}
	return true
}

func adminAuthorizationDebugPageSize(w http.ResponseWriter, r *http.Request) (int, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("pageSize"))
	if value == "" {
		return adminAuthorizationDebugDefaultPageSize, true
	}
	pageSize, err := strconv.Atoi(value)
	if err != nil || pageSize <= 0 {
		writeError(w, http.StatusBadRequest, "pageSize must be a positive integer")
		return 0, false
	}
	if pageSize > adminAuthorizationDebugMaximumPageSize {
		pageSize = adminAuthorizationDebugMaximumPageSize
	}
	return pageSize, true
}

func adminAuthorizationModelFromRef(model *core.AuthorizationModelRef) adminAuthorizationModelRecord {
	record := adminAuthorizationModelRecord{}
	if model == nil {
		return record
	}
	record.ID = strings.TrimSpace(model.GetId())
	record.Version = strings.TrimSpace(model.GetVersion())
	if createdAt := model.GetCreatedAt(); createdAt != nil {
		if ts := createdAt.AsTime(); !ts.IsZero() {
			record.CreatedAt = ts.UTC().Format(time.RFC3339)
		}
	}
	return record
}

func adminAuthorizationRelationshipFromProvider(rel *core.Relationship) adminAuthorizationRelationshipRecord {
	if rel == nil {
		return adminAuthorizationRelationshipRecord{}
	}
	subject := rel.GetSubject()
	resource := rel.GetResource()
	return adminAuthorizationRelationshipRecord{
		Subject: adminAuthorizationRef{
			Type: strings.TrimSpace(subject.GetType()),
			ID:   strings.TrimSpace(subject.GetId()),
		},
		Relation: strings.TrimSpace(rel.GetRelation()),
		Resource: adminAuthorizationRef{
			Type: strings.TrimSpace(resource.GetType()),
			ID:   strings.TrimSpace(resource.GetId()),
		},
		Managed: authorization.IsManagedProviderRelationship(rel),
	}
}

func adminAuthorizationDynamicRowDedupeKey(row adminAuthorizationMemberRow) string {
	return strings.Join([]string{row.Plugin, row.Role, row.SelectorKind, row.SelectorValue}, "\x00")
}
