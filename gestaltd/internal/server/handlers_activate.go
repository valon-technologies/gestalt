package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

const (
	activationFleetReadyTimeout = 10 * time.Minute
	activationFleetPollInterval = 500 * time.Millisecond
)

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
	waitForReady := false
	if query.Has("wait_for_ready") {
		values := query["wait_for_ready"]
		if len(values) != 1 || strings.TrimSpace(values[0]) != "true" {
			writeError(w, http.StatusBadRequest, "wait_for_ready must be true when provided")
			return
		}
		waitForReady = true
	}
	var revision string
	if waitForReady {
		if len(minimumHealthyInstances) != 1 {
			writeError(w, http.StatusBadRequest, "minimum_healthy_instances is required when wait_for_ready is true")
			return
		}
		values := query["revision_name"]
		if len(values) != 1 {
			writeError(w, http.StatusBadRequest, "revision_name must be provided once when wait_for_ready is true")
			return
		}
		revision = strings.TrimSpace(values[0])
		if revision == "" {
			writeError(w, http.StatusBadRequest, "revision_name must not be blank")
			return
		}
		if s.revision == "" {
			writeError(w, http.StatusServiceUnavailable, "gestaltd revision identity is unavailable")
			return
		}
		if revision != s.revision {
			writeError(w, http.StatusConflict, "revision_name does not match this gestaltd revision")
			return
		}
		if s.instanceHeartbeats == nil {
			writeError(w, http.StatusServiceUnavailable, "gestaltd instance heartbeat service is unavailable")
			return
		}
	} else if query.Has("revision_name") {
		writeError(w, http.StatusBadRequest, "revision_name requires wait_for_ready=true")
		return
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
	if waitForReady {
		waitCtx, cancel := context.WithTimeout(r.Context(), activationFleetReadyTimeout)
		defer cancel()
		if err := s.waitForActivatedFleet(waitCtx, minimumHealthyInstances[0], revision); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				writeError(w, http.StatusGatewayTimeout, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) waitForActivatedFleet(ctx context.Context, minimumHealthy int, revision string) error {
	ticker := time.NewTicker(activationFleetPollInterval)
	defer ticker.Stop()
	for {
		heartbeats, err := s.instanceHeartbeats.ListFreshBySourceVersion(
			ctx,
			s.sourceVersion,
			s.now().Add(-s.appRegistryHeartbeatTTL),
		)
		if err != nil {
			return fmt.Errorf("list activated instances: %w", err)
		}
		ready := 0
		for _, heartbeat := range heartbeats {
			if heartbeat != nil &&
				strings.TrimSpace(heartbeat.Revision) == revision &&
				appregistry.HeartbeatReady(heartbeat) {
				ready++
			}
		}
		if ready >= minimumHealthy {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"wait for activated fleet: %d of %d instances became ready: %w",
				ready,
				minimumHealthy,
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}
