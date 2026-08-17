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
	"github.com/valon-technologies/gestalt/server/internal/coredata"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type appAdminRegistryPublishCreateRequest struct {
	Declaration *appregistry.PublishDeclaration `json:"declaration"`
}

type appAdminRegistryPublishResponse struct {
	PublishID         string                          `json:"publishId"`
	App               string                          `json:"app"`
	Registry          string                          `json:"registry"`
	Version           string                          `json:"version"`
	State             string                          `json:"state"`
	Uploads           []appAdminRegistryPublishUpload `json:"uploads,omitempty"`
	MissingUploads    []string                        `json:"missingUploads,omitempty"`
	MismatchedUploads []string                        `json:"mismatchedUploads,omitempty"`
	FailureReason     string                          `json:"failureReason,omitempty"`
	PublishedAt       string                          `json:"publishedAt,omitempty"`
	Publisher         string                          `json:"publisher,omitempty"`
	Renewed           bool                            `json:"renewed,omitempty"`
}

type appAdminRegistryPublishUpload struct {
	Platform  string            `json:"platform"`
	UploadURL string            `json:"uploadUrl"`
	ExpiresAt string            `json:"expiresAt"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type appAdminRegistryPublishSessionSummary struct {
	PublishID     string `json:"publishId"`
	Version       string `json:"version"`
	State         string `json:"state"`
	StartedAt     string `json:"startedAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	Publisher     string `json:"publisher,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
}

func (s *Server) mountAppAdminRegistryPublishRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/publishes", s.createAppAdminRegistryPublish)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Get("/apps/{app}/admin/registry/publishes/{publishID}", s.getAppAdminRegistryPublish)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/publishes/{publishID}/finalize", s.finalizeAppAdminRegistryPublish)
}

func (s *Server) createAppAdminRegistryPublish(w http.ResponseWriter, r *http.Request) {
	app, registryConfig, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	service, storageRoot, publicRoot, ok := s.appAdminPublishService(w, r, registryConfig)
	if !ok {
		return
	}
	var request appAdminRegistryPublishCreateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if request.Declaration == nil {
		writeError(w, http.StatusBadRequest, "declaration is required")
		return
	}
	subjectID, ok := s.appAdminPublisherSubjectID(w, r)
	if !ok {
		return
	}
	result, err := service.Create(r.Context(), appregistry.CreatePublishSessionInput{
		App:                app.name,
		Registry:           app.registry,
		StorageRoot:        storageRoot,
		PublicRoot:         publicRoot,
		DisplayName:        appDisplayName(s, app.name, request.Declaration),
		Description:        appDescription(s, app.name, request.Declaration),
		PublisherSubjectID: subjectID,
		Declaration:        request.Declaration,
	})
	if err != nil {
		writeAppAdminRegistryPublishError(w, err)
		return
	}
	s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, result.Session.ID, "app.registry.publish.create", true, "")
	writeJSON(w, http.StatusOK, appAdminRegistryPublishResponseFromSession(r.Context(), s, result.Session, result.Renewed))
}

func (s *Server) getAppAdminRegistryPublish(w http.ResponseWriter, r *http.Request) {
	app, registryConfig, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	service, storageRoot, publicRoot, ok := s.appAdminPublishService(w, r, registryConfig)
	if !ok {
		return
	}
	publishID := strings.TrimSpace(chi.URLParam(r, "publishID"))
	if publishID == "" {
		writeError(w, http.StatusBadRequest, "publishID is required")
		return
	}
	status, err := service.Status(r.Context(), app.name, publishID, storageRoot, publicRoot)
	if err != nil {
		writeAppAdminRegistryPublishError(w, err)
		return
	}
	response := appAdminRegistryPublishResponseFromSession(r.Context(), s, status.Session, false)
	response.MissingUploads = append([]string(nil), status.MissingUploads...)
	response.MismatchedUploads = append([]string(nil), status.MismatchedUploads...)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) finalizeAppAdminRegistryPublish(w http.ResponseWriter, r *http.Request) {
	app, registryConfig, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	service, storageRoot, publicRoot, ok := s.appAdminPublishService(w, r, registryConfig)
	if !ok {
		return
	}
	publishID := strings.TrimSpace(chi.URLParam(r, "publishID"))
	if publishID == "" {
		writeError(w, http.StatusBadRequest, "publishID is required")
		return
	}
	subjectID, ok := s.appAdminPublisherSubjectID(w, r)
	if !ok {
		return
	}
	result, err := service.Finalize(r.Context(), appregistry.FinalizePublishSessionInput{
		App:             app.name,
		PublishID:       publishID,
		StorageRoot:     storageRoot,
		PublicRoot:      publicRoot,
		DisplayName:     appDisplayName(s, app.name, nil),
		Description:     appDescription(s, app.name, nil),
		GestaltdVersion: strings.TrimSpace(s.sourceVersion),
	})
	if err != nil {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", false, err.Error())
		writeAppAdminRegistryPublishError(w, err)
		return
	}
	s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", true, "")
	s.notifyAppAutoDeploy(app.name)
	writeJSON(w, http.StatusOK, appAdminRegistryPublishResponseFromSession(r.Context(), s, result.Session, false))
}

func (s *Server) appAdminPublishService(w http.ResponseWriter, r *http.Request, registry config.AppRegistryConfig) (*appregistry.PublishSessionService, string, string, bool) {
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

func appAdminRegistryPublishResponseFromSession(ctx context.Context, s *Server, session *core.AppRegistryPublishSession, renewed bool) appAdminRegistryPublishResponse {
	response := appAdminRegistryPublishResponse{
		PublishID: session.ID,
		App:       session.App,
		Registry:  session.Registry,
		Version:   session.Version,
		State:     string(session.State),
		Renewed:   renewed,
	}
	for _, lease := range session.UploadLeases {
		response.Uploads = append(response.Uploads, appAdminRegistryPublishUpload{
			Platform:  lease.Platform,
			UploadURL: lease.UploadURL,
			ExpiresAt: formatAdminTime(lease.ExpiresAt),
			Headers:   appregistry.SignedUploadHeadersForResponse(lease.UploadHeaders),
		})
	}
	if session.State == core.AppRegistryPublishSessionFailed {
		response.FailureReason = session.FailureReason
	}
	if !session.PublishedAt.IsZero() {
		response.PublishedAt = formatAdminTime(session.PublishedAt)
	}
	if s != nil {
		response.Publisher = s.resolveSubjectDisplayLabel(ctx, session.PublisherSubjectID)
	}
	return response
}

func appAdminRegistryPublishSessionSummaryFromCore(ctx context.Context, s *Server, session *core.AppRegistryPublishSession) appAdminRegistryPublishSessionSummary {
	if session == nil {
		return appAdminRegistryPublishSessionSummary{}
	}
	summary := appAdminRegistryPublishSessionSummary{
		PublishID: session.ID,
		Version:   session.Version,
		State:     string(session.State),
		StartedAt: formatAdminTime(session.PublishStartedAt),
		UpdatedAt: formatAdminTime(session.UpdatedAt),
	}
	if session.State == core.AppRegistryPublishSessionFailed {
		summary.FailureReason = session.FailureReason
	}
	if s != nil {
		summary.Publisher = s.resolveSubjectDisplayLabel(ctx, session.PublisherSubjectID)
	}
	return summary
}

func writeAppAdminRegistryPublishError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, core.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, appregistry.ErrPublishSessionUnavailable):
		status = http.StatusServiceUnavailable
	case errors.Is(err, appregistry.ErrPublishDeclarationInvalid),
		errors.Is(err, appregistry.ErrPublishRequiredPlatform),
		errors.Is(err, appregistry.ErrPublishArtifactLimit),
		errors.Is(err, appregistry.ErrPublishAppIdentityMismatch),
		errors.Is(err, appregistry.ErrPublishUploadMissing):
		status = http.StatusBadRequest
	case errors.Is(err, appregistry.ErrPublishVersionConflict),
		errors.Is(err, appregistry.ErrPublishUploadMismatch),
		errors.Is(err, appregistry.ErrPublishFinalizeInProgress),
		errors.Is(err, coredata.ErrPublishSessionFinalizeConflict),
		errors.Is(err, coredata.ErrPublishSessionVersionLocked):
		status = http.StatusConflict
	case errors.Is(err, appregistry.ErrPublishSessionFailed):
		status = http.StatusConflict
	case errors.Is(err, appregistry.ErrPublishRegistryNotEnrolled):
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
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
		TargetKind: "app_registry_publish_session",
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
