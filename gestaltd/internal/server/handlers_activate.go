package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

const defaultActivateWarmupTimeout = 2 * time.Minute

func (s *Server) mountActivateRoute(r chi.Router) {
	r.Post("/activate", s.activateAppProvidersHandler)
}

func (s *Server) activateAppProvidersHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	retryParam := strings.TrimSpace(query.Get("retry"))
	if retryParam != "" && retryParam != "true" {
		writeError(w, http.StatusBadRequest, "retry must be true when provided")
		return
	}
	var minimumHealthyInstances []int
	if query.Has("minimum_healthy_instances") {
		values := query["minimum_healthy_instances"]
		if len(values) != 1 {
			writeError(w, http.StatusBadRequest, "minimum_healthy_instances must be provided once")
			return
		}
		minimumHealthy, err := strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil || minimumHealthy <= 0 {
			writeError(w, http.StatusBadRequest, "minimum_healthy_instances must be a positive integer")
			return
		}
		minimumHealthyInstances = []int{minimumHealthy}
	}
	if s.sourceVersion != "" {
		expectedSourceVersion := strings.TrimSpace(query.Get("source_version"))
		if expectedSourceVersion == "" {
			writeError(w, http.StatusBadRequest, "source_version is required")
			return
		}
		if expectedSourceVersion != s.sourceVersion {
			writeError(w, http.StatusConflict, "source_version does not match this gestaltd revision")
			return
		}
		if s.gestaltdSourceVersions == nil {
			writeError(w, http.StatusServiceUnavailable, "gestaltd source version service is unavailable")
			return
		}
		_, err := s.gestaltdSourceVersions.ActivateWithRolloutMode(
			r.Context(),
			s.sourceVersion,
			s.now(),
			retryParam == "true",
			appregistry.DefaultRolloutEnrollmentWindow,
			appregistry.DefaultRolloutTimeout,
			core.AppRolloutMode(s.appRegistryRolloutMode),
			minimumHealthyInstances...,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if s.activateAppProviders != nil {
		s.activateAppProviders(r.Context())
	}
	warmupTimeout := defaultActivateWarmupTimeout
	if query.Has("warmup_timeout_seconds") {
		seconds, err := strconv.Atoi(strings.TrimSpace(query.Get("warmup_timeout_seconds")))
		if err != nil || seconds <= 0 {
			writeError(w, http.StatusBadRequest, "warmup_timeout_seconds must be a positive integer")
			return
		}
		warmupTimeout = time.Duration(seconds) * time.Second
	}
	if s.waitAppProvidersReady != nil {
		waitCtx, cancel := context.WithTimeout(r.Context(), warmupTimeout)
		defer cancel()
		if err := s.waitAppProvidersReady(waitCtx); err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
	}
	routes := probeServingRoutes(s, servingWarmupPaths(s.appDefs, s.mountedUIs))
	failures := servingRouteFailures(routes)
	if len(failures) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, activateWarmupResponse{
			Status:     "routes_not_ready",
			InstanceID: appregistry.ResolveInstanceID(),
			Routes:     routes,
		})
		return
	}
	writeJSON(w, http.StatusOK, activateWarmupResponse{
		Status:     "ok",
		InstanceID: appregistry.ResolveInstanceID(),
		Routes:     routes,
	})
}
