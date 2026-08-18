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
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

func (s *Server) mountAppAdminRegistryPublishRoutes(r chi.Router) {
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/publishes", s.beginAppAdminRegistryPublish)
	r.With(s.pluginRouteAuthMiddleware("app"), s.appAdminAuthorizationMiddleware).
		Post("/apps/{app}/admin/registry/publishes/{publishID}/finalize", s.finalizeAppAdminRegistryPublish)
}

func (s *Server) beginAppAdminRegistryPublish(w http.ResponseWriter, r *http.Request) {
	app, _, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	subjectID, ok := s.appAdminPublisherSubjectID(w, r)
	if !ok {
		return
	}
	service, ok := s.appAdminPublishService(w)
	if !ok {
		return
	}
	if !s.appAdminPublishAllowed(w, app.name) {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, "", "app.registry.publish.begin", false, appregistry.ErrPublishAppNotAllowlisted.Error())
		return
	}
	declaration, decodeErr := s.decodePublishDeclaration(w, r)
	if decodeErr != "" {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, "", "app.registry.publish.begin", false, decodeErr)
		return
	}
	result, err := service.Begin(r.Context(), app.registry, appregistry.AdminPublishInput{
		App: app.name, Declaration: declaration,
	})
	if err != nil {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, "", "app.registry.publish.begin", false, err.Error())
		writeError(w, appregistry.PublishHTTPStatus(err), err.Error())
		return
	}
	s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, result.PublishID, "app.registry.publish.begin", true, "")
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) finalizeAppAdminRegistryPublish(w http.ResponseWriter, r *http.Request) {
	app, _, ok := s.appAdminRegistryConfig(w, r)
	if !ok {
		return
	}
	subjectID, ok := s.appAdminPublisherSubjectID(w, r)
	if !ok {
		return
	}
	service, ok := s.appAdminPublishService(w)
	if !ok {
		return
	}
	publishID := strings.TrimSpace(chi.URLParam(r, "publishID"))
	if publishID == "" {
		writeError(w, http.StatusBadRequest, "publishID is required")
		return
	}
	if !s.appAdminPublishAllowed(w, app.name) {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", false, appregistry.ErrPublishAppNotAllowlisted.Error())
		return
	}
	declaration, decodeErr := s.decodePublishDeclaration(w, r)
	if decodeErr != "" {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", false, decodeErr)
		return
	}
	displayName, description := app.name, ""
	if declaration.Manifest != nil {
		if v := strings.TrimSpace(declaration.Manifest.DisplayName); v != "" {
			displayName = v
		}
		if v := strings.TrimSpace(declaration.Manifest.Description); v != "" {
			description = v
		}
	}
	if s.pluginDefs != nil {
		if entry, ok := s.pluginDefs[app.name]; ok && entry != nil {
			if displayName == app.name && strings.TrimSpace(entry.DisplayName) != "" {
				displayName = strings.TrimSpace(entry.DisplayName)
			}
			if description == "" {
				description = strings.TrimSpace(entry.Description)
			}
		}
	}
	result, err := service.Finalize(r.Context(), app.registry, appregistry.AdminPublishInput{
		App: app.name, PublishID: publishID,
		DisplayName: displayName, Description: description,
		Declaration: declaration,
	})
	if err != nil {
		s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", false, err.Error())
		writeError(w, appregistry.PublishHTTPStatus(err), err.Error())
		return
	}
	s.auditAppRegistryPublish(r.Context(), r, subjectID, app.name, publishID, "app.registry.publish.finalize", true, "")
	s.notifyAppAutoDeploy(app.name)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) decodePublishDeclaration(w http.ResponseWriter, r *http.Request) (*appregistry.PublishDeclaration, string) {
	var body struct {
		Declaration *appregistry.PublishDeclaration `json:"declaration"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, "invalid JSON body"
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, "invalid JSON body"
	}
	if body.Declaration == nil {
		writeError(w, http.StatusBadRequest, "declaration is required")
		return nil, "declaration is required"
	}
	return body.Declaration, ""
}

func (s *Server) appAdminPublishService(w http.ResponseWriter) (*appregistry.StatelessPublishService, bool) {
	if s.appRegistryPublish == nil {
		writeError(w, http.StatusServiceUnavailable, "app registry publish is unavailable")
		return nil, false
	}
	return s.appRegistryPublish, true
}

func (s *Server) appAdminPublishAllowed(w http.ResponseWriter, app string) bool {
	if len(s.appRegistryPublishAllowedApps) == 0 {
		writeError(w, http.StatusServiceUnavailable, appregistry.ErrPublishUnavailable.Error())
		return false
	}
	if _, ok := s.appRegistryPublishAllowedApps[app]; !ok {
		writeError(w, appregistry.PublishHTTPStatus(appregistry.ErrPublishAppNotAllowlisted), appregistry.ErrPublishAppNotAllowlisted.Error())
		return false
	}
	return true
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

func (s *Server) auditAppRegistryPublish(ctx context.Context, r *http.Request, subjectID, app, publishID, operation string, allowed bool, errMsg string) {
	if s == nil || s.auditSink == nil {
		return
	}
	entry := core.AuditEntry{
		Timestamp: time.Now().UTC(), Source: "gestaltd", SubjectID: subjectID,
		CallerApp: app, TargetID: publishID, TargetKind: "app_registry_publish",
		TargetName: app, Operation: operation, Allowed: allowed, Error: errMsg,
	}
	if r != nil {
		entry.RequestID = strings.TrimSpace(r.Header.Get("X-Request-ID"))
		entry.ClientIP = invocation.ClientIP(r)
		entry.RemoteAddr = r.RemoteAddr
		entry.UserAgent = r.UserAgent()
	}
	s.auditSink.Log(ctx, entry)
	slog.Info("app registry publish", "operation", operation, "app", app, "publish_id", publishID, "subject_id", subjectID, "allowed", allowed, "error", errMsg)
}
