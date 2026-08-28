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
	case strings.HasPrefix(path, "/scim/v2/Users/"):
		h.user(w, r, clientID, strings.TrimPrefix(path, "/scim/v2/Users/"))
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
	schema := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": UserSchemaURN, "name": "User", "description": "Gestalt provisioned user",
		"attributes": []map[string]any{
			{"name": "externalId", "type": "string", "multiValued": false, "required": false, "caseExact": false, "mutability": "readWrite", "returned": "default", "uniqueness": "server"},
			{"name": "userName", "type": "string", "multiValued": false, "required": true, "caseExact": false, "mutability": "readWrite", "returned": "always", "uniqueness": "server"},
			{"name": "active", "type": "boolean", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default"},
			{"name": "displayName", "type": "string", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default"},
			{"name": "name", "type": "complex", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{{"name": "formatted", "type": "string"}, {"name": "familyName", "type": "string"}, {"name": "givenName", "type": "string"}}},
			{"name": "emails", "type": "complex", "multiValued": true, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{{"name": "value", "type": "string"}, {"name": "type", "type": "string", "canonicalValues": []string{"work"}}, {"name": "primary", "type": "boolean"}}},
		},
		"meta": map[string]any{"resourceType": "Schema", "location": h.service.baseURL + "/scim/v2/Schemas"},
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": []string{ListSchemaURN}, "totalResults": 1, "startIndex": 1, "itemsPerPage": 1, "Resources": []any{schema}})
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request, clientID string) {
	startIndex, err := paginationQueryInt(r, "startIndex", 1, 1)
	if err != nil {
		writeError(w, err)
		return
	}
	count, err := paginationQueryInt(r, "count", 100, 0)
	if err != nil {
		writeError(w, err)
		return
	}
	if count > 200 {
		count = 200
	}
	response, serviceErr := h.service.list(r.Context(), clientID, r.URL.Query().Get("filter"), startIndex, count)
	if serviceErr != nil {
		writeError(w, serviceErr)
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

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request, clientID string) {
	var input userInput
	if err := decodeBody(r, &input); err != nil {
		writeError(w, err)
		return
	}
	user, err := h.service.Create(r.Context(), clientID, input)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Location", user.Meta.Location)
	w.Header().Set("ETag", user.Meta.Version)
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request, clientID, id string) {
	if id == "" || strings.Contains(id, "/") {
		writeError(w, notFound())
		return
	}
	switch r.Method {
	case http.MethodGet:
		user, err := h.service.Get(r.Context(), clientID, id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeUser(w, http.StatusOK, user)
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
		writeUser(w, http.StatusOK, user)
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
		writeUser(w, http.StatusOK, user)
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

func writeUser(w http.ResponseWriter, status int, user *User) {
	w.Header().Set("Location", user.Meta.Location)
	w.Header().Set("ETag", user.Meta.Version)
	writeJSON(w, status, user)
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
