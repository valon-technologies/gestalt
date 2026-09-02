package scim

import (
	"encoding/json"
	"net/http"
	"strings"
)

type leanHandler struct{ s *CompactService }

func NewHandler(s *Service) http.Handler {
	if s == nil {
		return &leanHandler{}
	}
	return &leanHandler{s: s.compact}
}
func (h *leanHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cid, ok := h.auth(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="Gestalt SCIM"`)
		writeError(w, &Error{Status: 401, Detail: "A valid SCIM bearer token is required"})
		return
	}
	p := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case p == "/scim/v2/ServiceProviderConfig" && r.Method == http.MethodGet:
		h.serviceProviderConfig(w)
	case p == "/scim/v2/Schemas" && r.Method == http.MethodGet:
		h.schemas(w, "")
	case strings.HasPrefix(p, "/scim/v2/Schemas/") && r.Method == http.MethodGet:
		h.schemas(w, strings.TrimPrefix(p, "/scim/v2/Schemas/"))
	case p == "/scim/v2/ResourceTypes" && r.Method == http.MethodGet:
		h.resourceTypes(w, "")
	case strings.HasPrefix(p, "/scim/v2/ResourceTypes/") && r.Method == http.MethodGet:
		h.resourceTypes(w, strings.TrimPrefix(p, "/scim/v2/ResourceTypes/"))
	case p == "/scim/v2/Users" && r.Method == http.MethodGet:
		h.listUsers(w, r, cid)
	case p == "/scim/v2/Users" && r.Method == http.MethodPost:
		h.createUser(w, r, cid)
	case strings.HasPrefix(p, "/scim/v2/Users/"):
		h.user(w, r, cid, strings.TrimPrefix(p, "/scim/v2/Users/"))
	case p == "/scim/v2/Groups" && r.Method == http.MethodGet:
		h.listGroups(w, r, cid)
	case p == "/scim/v2/Groups" && r.Method == http.MethodPost:
		h.createGroup(w, r, cid)
	case strings.HasPrefix(p, "/scim/v2/Groups/"):
		h.group(w, r, cid, strings.TrimPrefix(p, "/scim/v2/Groups/"))
	default:
		writeError(w, notFound())
	}
}

func (h *leanHandler) serviceProviderConfig(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"schemas": []string{ServiceProviderConfigSchemaURN},
		// PATCH returns 501 until AuthorizationProvider gains a multi-tuple
		// transaction primitive that can satisfy RFC 7644 atomicity.
		"patch": map[string]any{"supported": false},
		"bulk":  map[string]any{"supported": false},
		// The endpoint accepts the documented equality/conjunction subset; it is
		// not the complete RFC filter grammar.
		"filter":         map[string]any{"supported": false, "maxResults": 200},
		"changePassword": map[string]any{"supported": false},
		"sort":           map[string]any{"supported": false},
		"etag":           map[string]any{"supported": true},
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer Token", "description": "Deployment-configured SCIM bearer token",
		}},
	})
}

func (h *leanHandler) resourceTypes(w http.ResponseWriter, id string) {
	resources := []map[string]any{
		{"schemas": []string{ResourceTypeSchemaURN}, "id": "User", "name": "User", "endpoint": "/Users", "schema": UserSchemaURN, "meta": map[string]any{"resourceType": "ResourceType", "location": h.s.baseURL + "/scim/v2/ResourceTypes/User"}},
		{"schemas": []string{ResourceTypeSchemaURN}, "id": "Group", "name": "Group", "endpoint": "/Groups", "schema": GroupSchemaURN, "meta": map[string]any{"resourceType": "ResourceType", "location": h.s.baseURL + "/scim/v2/ResourceTypes/Group"}},
	}
	if id != "" {
		for _, resource := range resources {
			if resource["id"] == id {
				writeJSON(w, http.StatusOK, resource)
				return
			}
		}
		writeError(w, notFoundResource("SCIM ResourceType"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"schemas": []string{ListSchemaURN}, "totalResults": len(resources), "startIndex": 1, "itemsPerPage": len(resources), "Resources": resources})
}
func (h *leanHandler) auth(r *http.Request) (string, bool) {
	if h.s == nil {
		return "", false
	}
	v := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(v) < 7 || !strings.EqualFold(v[:7], "Bearer ") {
		return "", false
	}
	return h.s.ClientForToken(strings.TrimSpace(v[7:]))
}
func (h *leanHandler) schemas(w http.ResponseWriter, schemaID string) {
	stringAttribute := func(name, description string, required bool, mutability, returned, uniqueness string, caseExact ...bool) map[string]any {
		exact := false
		if len(caseExact) > 0 {
			exact = caseExact[0]
		}
		return map[string]any{
			"name": name, "description": description, "type": "string", "multiValued": false,
			"required": required, "caseExact": exact, "mutability": mutability, "returned": returned, "uniqueness": uniqueness,
		}
	}
	userSchema := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": UserSchemaURN, "name": "User", "description": "Gestalt provisioned user",
		"attributes": []map[string]any{
			stringAttribute("userName", "Unique identifier for the User", true, "readWrite", "default", "server"),
			{"name": "active", "description": "Whether the User is active", "type": "boolean", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default"},
			stringAttribute("displayName", "Name of the User suitable for display", false, "readWrite", "default", "none"),
			{"name": "name", "description": "The components of the User's name", "type": "complex", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{
				stringAttribute("formatted", "The full name", false, "readWrite", "default", "none"),
				stringAttribute("familyName", "The family name", false, "readWrite", "default", "none"),
				stringAttribute("givenName", "The given name", false, "readWrite", "default", "none"),
			}},
			{"name": "emails", "description": "Email addresses for the User", "type": "complex", "multiValued": true, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{
				stringAttribute("value", "Email address", true, "readWrite", "default", "none"),
				{"name": "type", "description": "Type of email address", "type": "string", "multiValued": false, "required": false, "caseExact": false, "mutability": "readWrite", "returned": "default", "uniqueness": "none", "canonicalValues": []string{"work", "home", "other"}},
				{"name": "primary", "description": "Whether this is the primary email", "type": "boolean", "multiValued": false, "required": false, "mutability": "readWrite", "returned": "default"},
			}},
			{"name": "groups", "description": "Groups to which the User belongs", "type": "complex", "multiValued": true, "required": false, "mutability": "readOnly", "returned": "default", "subAttributes": []map[string]any{
				{"name": "value", "description": "Group identifier", "type": "string", "multiValued": false, "required": true, "caseExact": false, "mutability": "readOnly", "returned": "default", "uniqueness": "none"},
				{"name": "$ref", "description": "URI of the referenced Group", "type": "reference", "multiValued": false, "required": false, "mutability": "readOnly", "returned": "default", "referenceTypes": []string{"Group"}},
				stringAttribute("display", "Group display name", false, "readOnly", "default", "none"),
				{"name": "type", "description": "Membership type", "type": "string", "multiValued": false, "required": false, "mutability": "readOnly", "returned": "default", "canonicalValues": []string{"direct", "indirect"}},
			}},
		},
		"meta": map[string]any{"resourceType": "Schema", "location": h.s.baseURL + "/scim/v2/Schemas/" + UserSchemaURN},
	}
	groupSchema := map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"}, "id": GroupSchemaURN, "name": "Group", "description": "Gestalt provisioned group",
		"attributes": []map[string]any{
			stringAttribute("displayName", "A human-readable name for the Group", true, "readWrite", "default", "none"),
			{"name": "members", "description": "Members of the Group", "type": "complex", "multiValued": true, "required": false, "mutability": "readWrite", "returned": "default", "subAttributes": []map[string]any{
				{"name": "value", "description": "Identifier of the member", "type": "string", "multiValued": false, "required": true, "caseExact": false, "mutability": "immutable", "returned": "default", "uniqueness": "none"},
				{"name": "$ref", "description": "URI of the referenced member", "type": "reference", "multiValued": false, "required": false, "mutability": "immutable", "returned": "default", "referenceTypes": []string{"User", "Group"}},
				stringAttribute("display", "Display name of the member", false, "readOnly", "default", "none"),
				{"name": "type", "description": "Member resource type", "type": "string", "multiValued": false, "required": false, "caseExact": false, "mutability": "immutable", "returned": "default", "uniqueness": "none", "canonicalValues": []string{"User", "Group"}},
			}},
		},
		"meta": map[string]any{"resourceType": "Schema", "location": h.s.baseURL + "/scim/v2/Schemas/" + GroupSchemaURN},
	}
	resources := []any{userSchema, groupSchema}
	if schemaID != "" {
		for _, resource := range resources {
			if resource.(map[string]any)["id"] == schemaID {
				location := resource.(map[string]any)["meta"].(map[string]any)["location"].(string)
				w.Header().Set("Content-Location", location)
				writeJSON(w, 200, resource)
				return
			}
		}
		writeError(w, notFoundResource("SCIM Schema"))
		return
	}
	writeJSON(w, 200, map[string]any{"schemas": []string{ListSchemaURN}, "totalResults": 2, "startIndex": 1, "itemsPerPage": 2, "Resources": resources})
}
func (h *leanHandler) listUsers(w http.ResponseWriter, r *http.Request, c string) {
	if err := validateProjectionQuery(r); err != nil {
		writeError(w, err)
		return
	}
	st, n, e := pagination(r)
	if e != nil {
		writeError(w, e)
		return
	}
	v, e := h.s.list(r.Context(), c, r.URL.Query().Get("filter"), st, n)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, projectRepresentation(v, r.URL.Query().Get("attributes"), r.URL.Query().Get("excludedAttributes")))
}
func (h *leanHandler) createUser(w http.ResponseWriter, r *http.Request, c string) {
	if err := validateProjectionQuery(r); err != nil {
		writeError(w, err)
		return
	}
	var in userInput
	if e := decodeBody(r, &in); e != nil {
		writeError(w, e)
		return
	}
	u, e := h.s.Create(r.Context(), c, in)
	if e != nil {
		writeError(w, e)
		return
	}
	writeResourceForRequest(w, r, 201, u)
}
func (h *leanHandler) user(w http.ResponseWriter, r *http.Request, c, id string) {
	if id == "" || strings.Contains(id, "/") {
		writeError(w, notFound())
		return
	}
	if err := validateProjectionQuery(r); err != nil {
		writeError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		u, e := h.s.Get(r.Context(), c, id)
		if e != nil {
			writeError(w, e)
			return
		}
		writeResourceForRequest(w, r, 200, u)
	case http.MethodPut:
		var in userInput
		if e := decodeBody(r, &in); e != nil {
			writeError(w, e)
			return
		}
		u, e := h.s.Replace(r.Context(), c, id, strings.TrimSpace(r.Header.Get("If-Match")), in)
		if e != nil {
			writeError(w, e)
			return
		}
		writeResourceForRequest(w, r, 200, u)
	case http.MethodPatch:
		writeError(w, &Error{Status: http.StatusNotImplemented, Detail: "SCIM PATCH requires an atomic authorization-provider mutation primitive"})
	case http.MethodDelete:
		if e := h.s.Delete(r.Context(), c, id, strings.TrimSpace(r.Header.Get("If-Match"))); e != nil {
			writeError(w, e)
			return
		}
		w.WriteHeader(204)
	default:
		writeError(w, notFound())
	}
}
func (h *leanHandler) listGroups(w http.ResponseWriter, r *http.Request, c string) {
	if err := validateProjectionQuery(r); err != nil {
		writeError(w, err)
		return
	}
	st, n, e := pagination(r)
	if e != nil {
		writeError(w, e)
		return
	}
	v, e := h.s.listGroups(r.Context(), c, r.URL.Query().Get("filter"), st, n)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, projectRepresentation(v, r.URL.Query().Get("attributes"), r.URL.Query().Get("excludedAttributes")))
}
func (h *leanHandler) createGroup(w http.ResponseWriter, r *http.Request, c string) {
	if err := validateProjectionQuery(r); err != nil {
		writeError(w, err)
		return
	}
	var in groupInput
	if e := decodeBody(r, &in); e != nil {
		writeError(w, e)
		return
	}
	g, e := h.s.CreateGroup(r.Context(), c, in)
	if e != nil {
		writeError(w, e)
		return
	}
	writeResourceForRequest(w, r, 201, g)
}
func (h *leanHandler) group(w http.ResponseWriter, r *http.Request, c, id string) {
	if id == "" || strings.Contains(id, "/") {
		writeError(w, notFoundResource("SCIM Group"))
		return
	}
	if err := validateProjectionQuery(r); err != nil {
		writeError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		g, e := h.s.GetGroup(r.Context(), c, id)
		if e != nil {
			writeError(w, e)
			return
		}
		writeResourceForRequest(w, r, 200, g)
	case http.MethodPut:
		var in groupInput
		if e := decodeBody(r, &in); e != nil {
			writeError(w, e)
			return
		}
		g, e := h.s.ReplaceGroup(r.Context(), c, id, strings.TrimSpace(r.Header.Get("If-Match")), in)
		if e != nil {
			writeError(w, e)
			return
		}
		writeResourceForRequest(w, r, 200, g)
	case http.MethodPatch:
		writeError(w, &Error{Status: http.StatusNotImplemented, Detail: "SCIM PATCH requires an atomic authorization-provider mutation primitive"})
	case http.MethodDelete:
		if e := h.s.DeleteGroup(r.Context(), c, id, strings.TrimSpace(r.Header.Get("If-Match"))); e != nil {
			writeError(w, e)
			return
		}
		w.WriteHeader(204)
	default:
		writeError(w, notFoundResource("SCIM Group"))
	}
}
func writeResourceForRequest(w http.ResponseWriter, r *http.Request, status int, v any) {
	location, version := resourceLocation(v), resourceVersion(v)
	w.Header().Set("Location", location)
	w.Header().Set("Content-Location", location)
	w.Header().Set("ETag", version)
	if r.Method == http.MethodGet && status == http.StatusOK && ifNoneMatch(r.Header.Get("If-None-Match"), version) {
		w.Header().Set("Content-Type", "application/scim+json")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, status, projectRepresentation(v, r.URL.Query().Get("attributes"), r.URL.Query().Get("excludedAttributes")))
}

func validateProjectionQuery(r *http.Request) error {
	if strings.TrimSpace(r.URL.Query().Get("attributes")) != "" && strings.TrimSpace(r.URL.Query().Get("excludedAttributes")) != "" {
		return invalid("attributes and excludedAttributes cannot both be supplied")
	}
	return nil
}

func resourceLocation(v any) string {
	switch value := v.(type) {
	case *User:
		return value.Meta.Location
	case *Group:
		return value.Meta.Location
	default:
		return ""
	}
}

func resourceVersion(v any) string {
	switch value := v.(type) {
	case *User:
		return value.Meta.Version
	case *Group:
		return value.Meta.Version
	default:
		return ""
	}
}

func ifNoneMatch(header, current string) bool {
	current = strings.TrimPrefix(strings.TrimSpace(current), "W/")
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate != "" && candidate == current {
			return true
		}
	}
	return false
}

func projectRepresentation(value any, attributes, excluded string) any {
	if strings.TrimSpace(attributes) == "" && strings.TrimSpace(excluded) == "" {
		return value
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return value
	}
	if resources, ok := object["Resources"].([]any); ok {
		for i := range resources {
			resources[i] = projectObject(resources[i], attributes, excluded)
		}
		object["Resources"] = resources
		return object
	}
	return projectObject(object, attributes, excluded)
}

func projectObject(object any, attributes, excluded string) any {
	value, ok := object.(map[string]any)
	if !ok {
		return object
	}
	resourceType := ""
	if meta, ok := value["meta"].(map[string]any); ok {
		resourceType, _ = meta["resourceType"].(string)
	}
	if strings.TrimSpace(attributes) != "" {
		selected := map[string]map[string]struct{}{"schemas": nil, "id": nil}
		for _, raw := range strings.Split(attributes, ",") {
			selector := normalizeProjectionSelector(raw, resourceType)
			if selector == "" {
				continue
			}
			parts := strings.SplitN(selector, ".", 2)
			if parts[0] != "" {
				name := strings.ToLower(parts[0])
				if _, ok := selected[name]; !ok {
					selected[name] = map[string]struct{}{}
				}
				if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
					if selected[name] == nil {
						selected[name] = map[string]struct{}{}
					}
					selected[name][strings.ToLower(strings.TrimSpace(parts[1]))] = struct{}{}
				}
			}
		}
		for key := range value {
			selection, keep := selected[strings.ToLower(key)]
			if !keep {
				delete(value, key)
				continue
			}
			if len(selection) > 0 {
				projectSubattributes(value[key], selection)
			}
		}
	}
	for _, raw := range strings.Split(excluded, ",") {
		selector := normalizeProjectionSelector(raw, resourceType)
		if selector == "" {
			continue
		}
		parts := strings.SplitN(strings.ToLower(selector), ".", 2)
		name := parts[0]
		if name == "" || name == "schemas" || name == "id" {
			continue
		}
		if len(parts) == 2 {
			for key, nested := range value {
				if strings.EqualFold(key, name) {
					removeSubattribute(nested, parts[1])
				}
			}
			continue
		}
		for key := range value {
			if strings.EqualFold(key, name) {
				delete(value, key)
			}
		}
	}
	return value
}

// normalizeProjectionSelector removes only the core schema prefix belonging
// to this resource. A User selector must not accidentally become a Group
// selector (or vice versa) when a client sends a schema-qualified path.
func normalizeProjectionSelector(raw, resourceType string) string {
	selector := strings.TrimSpace(raw)
	lower := strings.ToLower(selector)
	userPrefix := strings.ToLower(UserSchemaURN) + ":"
	groupPrefix := strings.ToLower(GroupSchemaURN) + ":"
	switch {
	case strings.HasPrefix(lower, userPrefix):
		if !strings.EqualFold(resourceType, "User") {
			return ""
		}
		return selector[len(userPrefix):]
	case strings.HasPrefix(lower, groupPrefix):
		if !strings.EqualFold(resourceType, "Group") {
			return ""
		}
		return selector[len(groupPrefix):]
	default:
		return selector
	}
}

func projectSubattributes(raw any, selected map[string]struct{}) {
	if object, ok := raw.(map[string]any); ok {
		for key := range object {
			if _, keep := selected[strings.ToLower(key)]; !keep {
				delete(object, key)
			}
		}
	}
	if many, ok := raw.([]any); ok {
		for _, item := range many {
			projectSubattributes(item, selected)
		}
	}
}

func removeSubattribute(raw any, name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if object, ok := raw.(map[string]any); ok {
		for key := range object {
			if strings.EqualFold(key, name) {
				delete(object, key)
			}
		}
	}
	if many, ok := raw.([]any); ok {
		for _, item := range many {
			removeSubattribute(item, name)
		}
	}
}
