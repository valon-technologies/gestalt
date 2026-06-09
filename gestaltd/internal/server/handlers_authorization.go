package server

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/encoding/protojson"
	gproto "google.golang.org/protobuf/proto"
)

func (s *Server) checkAuthorizationAccess(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthorizationProvider(w) {
		return
	}
	var req proto.CheckAccessRequest
	if !decodeProtoJSONBody(w, r, &req) {
		return
	}
	resp, err := s.authorization.CheckAccess(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *Server) listAuthorizationRelationships(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthorizationProvider(w) {
		return
	}
	req, ok := listAuthorizationRelationshipsRequestFromQuery(w, r)
	if !ok {
		return
	}
	resp, err := s.authorization.ListRelationships(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *Server) requireAuthorizationProvider(w http.ResponseWriter) bool {
	if s.authorization == nil {
		writeError(w, http.StatusPreconditionFailed, "authorization provider is not configured")
		return false
	}
	return true
}

func decodeProtoJSONBody(w http.ResponseWriter, r *http.Request, msg gproto.Message) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(w, http.StatusBadRequest, "request body is required")
		return false
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, msg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeProtoJSON(w http.ResponseWriter, status int, msg gproto.Message) {
	body, err := (protojson.MarshalOptions{EmitUnpopulated: false}).Marshal(msg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
}

func listAuthorizationRelationshipsRequestFromQuery(w http.ResponseWriter, r *http.Request) (*proto.ListRelationshipsRequest, bool) {
	query := r.URL.Query()
	pageSize, ok := parseOptionalInt32Query(w, queryValue(query, "pageSize", "page_size"), "pageSize")
	if !ok {
		return nil, false
	}
	targetType, ok := relationshipTargetTypeFromQuery(w, queryValue(query, "targetType", "target_type"))
	if !ok {
		return nil, false
	}
	sourceLayer, ok := sourceLayerFromQuery(w, queryValue(query, "sourceLayer", "source_layer"))
	if !ok {
		return nil, false
	}

	filter := &proto.RelationshipFilter{
		Relation:         strings.TrimSpace(query.Get("relation")),
		TargetType:       targetType,
		TargetEntityType: strings.TrimSpace(queryValue(query, "targetEntityType", "target_entity_type")),
		ResourceType:     strings.TrimSpace(queryValue(query, "resourceType", "resource_type")),
		SourceLayer:      sourceLayer,
	}
	if subjectID := strings.TrimSpace(queryValue(query, "subjectId", "subject_id", "targetSubjectId", "target_subject_id")); subjectID != "" {
		filter.Target = &proto.RelationshipTarget{
			Kind: &proto.RelationshipTarget_Subject{
				Subject: &proto.Subject{
					Type: strings.TrimSpace(queryValue(query, "subjectType", "subject_type", "targetSubjectType", "target_subject_type")),
					Id:   subjectID,
				},
			},
		}
	}
	if resourceID := strings.TrimSpace(queryValue(query, "resourceId", "resource_id")); resourceID != "" || filter.ResourceType != "" {
		filter.Resource = &proto.Resource{
			Type: strings.TrimSpace(queryValue(query, "resourceType", "resource_type")),
			Id:   resourceID,
		}
	}

	return &proto.ListRelationshipsRequest{
		Filter:    filter,
		PageSize:  pageSize,
		PageToken: strings.TrimSpace(queryValue(query, "pageToken", "page_token")),
	}, true
}

func parseOptionalInt32Query(w http.ResponseWriter, raw, name string) (int32, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s must be a non-negative integer", name))
		return 0, false
	}
	return int32(value), true
}

func relationshipTargetTypeFromQuery(w http.ResponseWriter, raw string) (proto.RelationshipTargetType, bool) {
	switch normalizeEnumQuery(raw) {
	case "":
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_UNSPECIFIED, true
	case "subject":
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_SUBJECT, true
	case "resource":
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_RESOURCE, true
	case "subject_set", "subjectset":
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_SUBJECT_SET, true
	default:
		writeError(w, http.StatusBadRequest, "targetType must be subject, resource, or subject_set")
		return proto.RelationshipTargetType_RELATIONSHIP_TARGET_TYPE_UNSPECIFIED, false
	}
}

func sourceLayerFromQuery(w http.ResponseWriter, raw string) (proto.SourceLayer, bool) {
	switch normalizeEnumQuery(raw) {
	case "":
		return proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED, true
	case "static_config", "staticconfig":
		return proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG, true
	case "runtime":
		return proto.SourceLayer_SOURCE_LAYER_RUNTIME, true
	default:
		writeError(w, http.StatusBadRequest, "sourceLayer must be static_config or runtime")
		return proto.SourceLayer_SOURCE_LAYER_UNSPECIFIED, false
	}
}

func normalizeEnumQuery(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimPrefix(raw, "relationship_target_type_")
	raw = strings.TrimPrefix(raw, "source_layer_")
	raw = strings.ReplaceAll(raw, "-", "_")
	return raw
}
