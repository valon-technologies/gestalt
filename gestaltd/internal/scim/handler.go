package scim

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) http.Handler { return &Handler{service: service} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clientID, ok := h.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="Gestalt SCIM"`)
		writeError(w, &Error{Status: http.StatusUnauthorized, Detail: "A valid SCIM bearer token is required"})
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/scim/v2/Schemas" && r.Method == http.MethodGet:
		h.schemas(w)
	case path == "/scim/v2/Users" && r.Method == http.MethodGet:
		h.listUsers(w, r, clientID)
	case path == "/scim/v2/Users" && r.Method == http.MethodPost:
		h.createUser(w, r, clientID)
	case path == "/scim/v2/Groups" && r.Method == http.MethodGet:
		h.listGroups(w, r, clientID)
	case path == "/scim/v2/Groups" && r.Method == http.MethodPost:
		h.createGroup(w, r, clientID)
	case strings.HasPrefix(path, "/scim/v2/Users/"):
		h.user(w, r, clientID, strings.TrimPrefix(path, "/scim/v2/Users/"))
	case strings.HasPrefix(path, "/scim/v2/Groups/"):
		h.group(w, r, clientID, strings.TrimPrefix(path, "/scim/v2/Groups/"))
	default:
		writeError(w, notFound())
	}
}

func (h *Handler) authenticate(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < 7 || !strings.EqualFold(header[:7], "Bearer ") {
		return "", false
	}
	return h.service.ClientForToken(strings.TrimSpace(header[7:]))
}

func (h *Handler) schemas(w http.ResponseWriter) {
	userSchema := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": UserSchemaURN, "name": "User", "description": "Gestalt provisioned user",
		"attributes": []map[string]any{
			{"name": "externalId", "type": "string", "multiValued": false, "required": false, "caseExact": false, "mutability": "readWrite", "returned": "default", "uniqueness": "server"},
			{"name": "userName", "type": "string", "multiValued": false, "required": true, "caseExact": false, "mutability": "readWrite", "returned": "always", "uniqueness": "server"},
			{"name": "active", "type": "boolean", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default"},
			{"name": "displayName", "type": "string", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default"},
			{"name": "name", "type": "complex", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{{"name": "formatted", "type": "string"}, {"name": "familyName", "type": "string"}, {"name": "givenName", "type": "string"}}},
			{"name": "emails", "type": "complex", "multiValued": true, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{{"name": "value", "type": "string"}, {"name": "type", "type": "string", "canonicalValues": []string{"work"}}, {"name": "primary", "type": "boolean"}}},
			{"name": "groups", "type": "complex", "multiValued": true, "required": false, "mutability": "readOnly", "returned": "default", "subAttributes": []map[string]any{{"name": "value", "type": "string"}, {"name": "$ref", "type": "reference"}, {"name": "display", "type": "string"}, {"name": "type", "type": "string", "canonicalValues": []string{"direct", "indirect"}}}},
		},
		"meta": map[string]any{"resourceType": "Schema", "location": h.service.baseURL + "/scim/v2/Schemas"},
	}
	groupSchema := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": GroupSchemaURN, "name": "Group", "description": "Gestalt provisioned group",
		"attributes": []map[string]any{
			{"name": "displayName", "type": "string", "multiValued": false, "required": true, "caseExact": false, "mutability": "readWrite", "returned": "always", "uniqueness": "none"},
			{"name": "members", "type": "complex", "multiValued": true, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{{"name": "value", "type": "string", "required": true, "mutability": "immutable"}, {"name": "$ref", "type": "reference", "mutability": "immutable"}, {"name": "display", "type": "string", "mutability": "readOnly"}, {"name": "type", "type": "string", "mutability": "immutable"}}},
		},
		"meta": map[string]any{"resourceType": "Schema", "location": h.service.baseURL + "/scim/v2/Schemas"},
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": []string{ListSchemaURN}, "totalResults": 2, "startIndex": 1, "itemsPerPage": 2, "Resources": []any{userSchema, groupSchema}})
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request, clientID string) {
	startIndex, count, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	response, serviceErr := h.service.list(r.Context(), clientID, r.URL.Query().Get("filter"), startIndex, count)
	if serviceErr != nil {
		writeError(w, serviceErr)
		return
	}
	if err := h.service.decorateUsers(r.Context(), clientID, response.Resources); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func paginationQueryInt(r *http.Request, name string, defaultValue, minimum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, parseErr := strconv.Atoi(raw)
	if parseErr != nil {
		return 0, invalid(name + " is invalid")
	}
	if value < minimum {
		value = minimum
	}
	return value, nil
}

func pagination(r *http.Request) (int, int, error) {
	startIndex, err := paginationQueryInt(r, "startIndex", 1, 1)
	if err != nil {
		return 0, 0, err
	}
	count, err := paginationQueryInt(r, "count", 100, 0)
	if err != nil {
		return 0, 0, err
	}
	return startIndex, min(count, 200), nil
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request, clientID string) {
	var input userInput
	if err := decodeBody(r, &input); err != nil {
		writeError(w, err)
		return
	}
	groups, err := h.service.loadGroupGraph(r.Context(), clientID)
	if err != nil {
		writeError(w, err)
		return
	}
	user, err := h.service.Create(r.Context(), clientID, input)
	if err != nil {
		writeError(w, err)
		return
	}
	h.writeUser(w, http.StatusCreated, user, groups)
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request, clientID, id string) {
	if id == "" || strings.Contains(id, "/") {
		writeError(w, notFound())
		return
	}
	var groups map[string]*persistedGroup
	if r.Method == http.MethodGet || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		var err error
		groups, err = h.service.loadGroupGraph(r.Context(), clientID)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	switch r.Method {
	case http.MethodGet:
		user, err := h.service.Get(r.Context(), clientID, id)
		if err != nil {
			writeError(w, err)
			return
		}
		h.writeUser(w, http.StatusOK, user, groups)
	case http.MethodPut:
		var input userInput
		if err := decodeBody(r, &input); err != nil {
			writeError(w, err)
			return
		}
		user, err := h.service.Replace(r.Context(), clientID, id, strings.TrimSpace(r.Header.Get("If-Match")), input)
		if err != nil {
			writeError(w, err)
			return
		}
		h.writeUser(w, http.StatusOK, user, groups)
	case http.MethodPatch:
		var request patchRequest
		if err := decodeBody(r, &request); err != nil {
			writeError(w, err)
			return
		}
		user, err := h.service.Patch(r.Context(), clientID, id, strings.TrimSpace(r.Header.Get("If-Match")), request)
		if err != nil {
			writeError(w, err)
			return
		}
		h.writeUser(w, http.StatusOK, user, groups)
	case http.MethodDelete:
		if err := h.service.Delete(r.Context(), clientID, id, strings.TrimSpace(r.Header.Get("If-Match"))); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, notFound())
	}
}

func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request, clientID string) {
	startIndex, count, err := pagination(r)
	if err != nil {
		writeError(w, err)
		return
	}
	response, serviceErr := h.service.listGroups(r.Context(), clientID, r.URL.Query().Get("filter"), startIndex, count)
	if serviceErr != nil {
		writeError(w, serviceErr)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request, clientID string) {
	var input groupInput
	if err := decodeBody(r, &input); err != nil {
		writeError(w, err)
		return
	}
	group, err := h.service.CreateGroup(r.Context(), clientID, input)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", group.Meta.Location)
	w.Header().Set("ETag", group.Meta.Version)
	writeJSON(w, http.StatusCreated, group)
}

func (h *Handler) group(w http.ResponseWriter, r *http.Request, clientID, id string) {
	if id == "" || strings.Contains(id, "/") {
		writeError(w, notFoundResource("SCIM Group"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		group, err := h.service.GetGroup(r.Context(), clientID, id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeGroup(w, http.StatusOK, group)
	case http.MethodPut:
		var input groupInput
		if err := decodeBody(r, &input); err != nil {
			writeError(w, err)
			return
		}
		group, err := h.service.ReplaceGroup(r.Context(), clientID, id, strings.TrimSpace(r.Header.Get("If-Match")), input)
		if err != nil {
			writeError(w, err)
			return
		}
		writeGroup(w, http.StatusOK, group)
	case http.MethodPatch:
		var request patchRequest
		if err := decodeBody(r, &request); err != nil {
			writeError(w, err)
			return
		}
		group, err := h.service.PatchGroup(r.Context(), clientID, id, strings.TrimSpace(r.Header.Get("If-Match")), request)
		if err != nil {
			writeError(w, err)
			return
		}
		writeGroup(w, http.StatusOK, group)
	case http.MethodDelete:
		if err := h.service.DeleteGroup(r.Context(), clientID, id, strings.TrimSpace(r.Header.Get("If-Match"))); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, notFoundResource("SCIM Group"))
	}
}

func decodeBody(r *http.Request, target any) error {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/scim+json" {
		return invalid("Content-Type must be application/scim+json")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		return invalid("request body is not valid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return invalid("request body must contain one JSON value")
	}
	return nil
}

func (h *Handler) writeUser(w http.ResponseWriter, status int, user *User, groups map[string]*persistedGroup) {
	user.Groups = h.service.groupRefsForUser(groups, user.ID)
	w.Header().Set("Location", user.Meta.Location)
	w.Header().Set("ETag", user.Meta.Version)
	writeJSON(w, status, user)
}

func writeGroup(w http.ResponseWriter, status int, group *Group) {
	w.Header().Set("Location", group.Meta.Location)
	w.Header().Set("ETag", group.Meta.Version)
	writeJSON(w, status, group)
}

func writeError(w http.ResponseWriter, err error) {
	scimErr := &Error{Status: http.StatusInternalServerError, Detail: "internal SCIM server error"}
	if !errors.As(err, &scimErr) {
		scimErr = &Error{Status: http.StatusServiceUnavailable, Detail: "SCIM service is temporarily unavailable", Retry: true}
	}
	if scimErr.Retry {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	}
	payload := map[string]any{"schemas": []string{ErrorSchemaURN}, "status": strconv.Itoa(scimErr.Status), "detail": scimErr.Detail}
	if scimErr.SCIMType != "" {
		payload["scimType"] = scimErr.SCIMType
	}
	writeJSON(w, scimErr.Status, payload)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
