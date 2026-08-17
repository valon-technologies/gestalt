package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type appAdminRegistryPublishRequest struct {
	Declaration *appregistry.PublishDeclaration `json:"declaration"`
}

type appAdminRegistryPublishResponse struct {
	PublishID   string                          `json:"publishId"`
	App         string                          `json:"app"`
	Registry    string                          `json:"registry"`
	Version     string                          `json:"version"`
	State       string                          `json:"state"`
	Uploads     []appAdminRegistryPublishUpload `json:"uploads,omitempty"`
	PublishedAt string                          `json:"publishedAt,omitempty"`
}

type appAdminRegistryPublishUpload struct {
	Platform  string            `json:"platform"`
	UploadURL string            `json:"uploadUrl"`
	ExpiresAt string            `json:"expiresAt"`
	Headers   map[string]string `json:"headers,omitempty"`
}

func (s *Server) mountAppAdminRegistryPublishRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/publishes", s.beginAppAdminRegistryPublish)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/publishes/{publishID}/finalize", s.finalizeAppAdminRegistryPublish)
}

func (s *Server) beginAppAdminRegistryPublish(w http.ResponseWriter, r *http.Request) {
	app, registryConfig, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	service, storageRoot, publicRoot, ok := s.appAdminPublishService(w, registryConfig)
	if !ok {
		return
	}
	request, ok := s.decodeAppAdminRegistryPublishRequest(w, r)
	if !ok {
		return
	}
	subjectID, ok := s.appAdminPublisherSubjectID(w, r)
	if !ok {
		return
	}
	result, err := service.Begin(r.Context(), appregistry.BeginPublishInput{
		App:         app.name,
		Registry:    app.registry,
		StorageRoot: storageRoot,
		PublicRoot:  publicRoot,
		Declaration: request.Declaration,
	})
	if err != nil {
		writeAppAdminRegistryPublishError(w, err)
		return
	}
	s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, result.PublishID, "app.registry.publish.begin", true, "")
	writeJSON(w, http.StatusOK, appAdminRegistryPublishResponseFromResult(result))
}

func (s *Server) finalizeAppAdminRegistryPublish(w http.ResponseWriter, r *http.Request) {
	app, registryConfig, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	service, storageRoot, publicRoot, ok := s.appAdminPublishService(w, registryConfig)
	if !ok {
		return
	}
	publishID := strings.TrimSpace(chi.URLParam(r, "publishID"))
	if publishID == "" {
		writeError(w, http.StatusBadRequest, "publishID is required")
		return
	}
	request, ok := s.decodeAppAdminRegistryPublishRequest(w, r)
	if !ok {
		return
	}
	subjectID, ok := s.appAdminPublisherSubjectID(w, r)
	if !ok {
		return
	}
	result, err := service.Finalize(r.Context(), appregistry.FinalizePublishInput{
		App:             app.name,
		PublishID:       publishID,
		Registry:        app.registry,
		StorageRoot:     storageRoot,
		PublicRoot:      publicRoot,
		DisplayName:     appDisplayName(s, app.name, request.Declaration),
		Description:     appDescription(s, app.name, request.Declaration),
		GestaltdVersion: strings.TrimSpace(s.sourceVersion),
		Declaration:     request.Declaration,
	})
	if err != nil {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", false, err.Error())
		writeAppAdminRegistryPublishError(w, err)
		return
	}
	s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", true, "")
	s.notifyAppAutoDeploy(app.name)
	writeJSON(w, http.StatusOK, appAdminRegistryPublishResponseFromResult(result))
}

func (s *Server) decodeAppAdminRegistryPublishRequest(w http.ResponseWriter, r *http.Request) (appAdminRegistryPublishRequest, bool) {
	var request appAdminRegistryPublishRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return appAdminRegistryPublishRequest{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return appAdminRegistryPublishRequest{}, false
	}
	if request.Declaration == nil {
		writeError(w, http.StatusBadRequest, "declaration is required")
		return appAdminRegistryPublishRequest{}, false
	}
	return request, true
}

func (s *Server) appAdminPublishService(w http.ResponseWriter, registry config.AppRegistryConfig) (*appregistry.StatelessPublishService, string, string, bool) {
	if s.appRegistryPublish == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry publish is unavailable")
		return nil, "", "", false
	}
	storageRoot, err := registry.StorageURL()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry is unavailable")
		return nil, "", "", false
	}
	publicRoot, err := registry.PublicURL()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "app registry is unavailable")
		return nil, "", "", false
	}
	return s.appRegistryPublish, storageRoot, publicRoot, true
}

func (s *Server) appAdminPublisherSubjectID(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return "", false
	}
	subjectID, err := principal.ResolveAuthorizationSubjectID(r.Context(), s.credentialUserResolver(), p)
	if err != nil || strings.TrimSpace(subjectID) == "" {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return "", false
	}
	return strings.TrimSpace(subjectID), true
}

func appAdminRegistryPublishResponseFromResult(result any) appAdminRegistryPublishResponse {
	switch typed := result.(type) {
	case *appregistry.BeginPublishResult:
		if typed == nil {
			return appAdminRegistryPublishResponse{}
		}
		return appAdminRegistryPublishResponseFromBegin(typed)
	case *appregistry.FinalizePublishResult:
		if typed == nil {
			return appAdminRegistryPublishResponse{}
		}
		return appAdminRegistryPublishResponseFromFinalize(typed)
	default:
		return appAdminRegistryPublishResponse{}
	}
}

func appAdminRegistryPublishResponseFromBegin(result *appregistry.BeginPublishResult) appAdminRegistryPublishResponse {
	response := appAdminRegistryPublishResponse{
		PublishID: result.PublishID,
		App:       result.App,
		Registry:  result.Registry,
		Version:   result.Version,
		State:     result.State,
	}
	for _, upload := range result.Uploads {
		response.Uploads = append(response.Uploads, appAdminRegistryPublishUpload{
			Platform:  upload.Platform,
			UploadURL: upload.UploadURL,
			ExpiresAt: formatAdminTime(upload.ExpiresAt),
			Headers:   appregistry.SignedUploadHeadersForResponse(upload.Headers),
		})
	}
	if !result.PublishedAt.IsZero() {
		response.PublishedAt = formatAdminTime(result.PublishedAt)
	}
	return response
}

func appAdminRegistryPublishResponseFromFinalize(result *appregistry.FinalizePublishResult) appAdminRegistryPublishResponse {
	response := appAdminRegistryPublishResponse{
		PublishID: result.PublishID,
		App:       result.App,
		Registry:  result.Registry,
		Version:   result.Version,
		State:     result.State,
	}
	if !result.PublishedAt.IsZero() {
		response.PublishedAt = formatAdminTime(result.PublishedAt)
	}
	return response
}

func writeAppAdminRegistryPublishError(w http.ResponseWriter, err error) {
	writeError(w, appregistry.PublishHTTPStatus(err), err.Error())
}

func (s *Server) auditAppRegistryPublish(ctx context.Context, r *http.Request, subjectID, app, publishID, operation string, allowed bool, errMsg string) {
	if s == nil || s.auditSink == nil {
		return
	}
	entry := core.AuditEntry{
		Timestamp:  time.Now().UTC(),
		Source:     "gestaltd",
		SubjectID:  subjectID,
		CallerApp:  app,
		TargetID:   publishID,
		TargetKind: "app_registry_publish",
		TargetName: app,
		Operation:  operation,
		Allowed:    allowed,
		Error:      errMsg,
	}
	if r != nil {
		entry.RequestID = strings.TrimSpace(r.Header.Get("X-Request-ID"))
		entry.ClientIP = invocation.ClientIP(r)
		entry.RemoteAddr = r.RemoteAddr
		entry.UserAgent = r.UserAgent()
	}
	s.auditSink.Log(ctx, entry)
	slog.Info("app registry publish",
		"operation", operation,
		"app", app,
		"publish_id", publishID,
		"subject_id", subjectID,
		"allowed", allowed,
		"error", errMsg,
	)
}

func appDisplayName(s *Server, appName string, declaration *appregistry.PublishDeclaration) string {
	if declaration != nil && declaration.Manifest != nil && strings.TrimSpace(declaration.Manifest.DisplayName) != "" {
		return strings.TrimSpace(declaration.Manifest.DisplayName)
	}
	if s != nil && s.pluginDefs != nil {
		if entry, ok := s.pluginDefs[appName]; ok && entry != nil {
			return strings.TrimSpace(entry.DisplayName)
		}
	}
	return appName
}

func appDescription(s *Server, appName string, declaration *appregistry.PublishDeclaration) string {
	if declaration != nil && declaration.Manifest != nil && strings.TrimSpace(declaration.Manifest.Description) != "" {
		return strings.TrimSpace(declaration.Manifest.Description)
	}
	if s != nil && s.pluginDefs != nil {
		if entry, ok := s.pluginDefs[appName]; ok && entry != nil {
			return strings.TrimSpace(entry.Description)
		}
	}
	return ""
}
