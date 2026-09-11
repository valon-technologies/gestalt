package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

type sharedActivationRequest struct {
	retry                   bool
	minimumHealthyInstances []int
}

func parseSharedActivationRequest(query map[string][]string) (sharedActivationRequest, string) {
	retryParam := strings.TrimSpace(firstQueryValue(query, "retry"))
	if retryParam != "" && retryParam != "true" {
		return sharedActivationRequest{}, "retry must be true when provided"
	}
	req := sharedActivationRequest{retry: retryParam == "true"}
	if _, ok := query["minimum_healthy_instances"]; ok {
		values := query["minimum_healthy_instances"]
		if len(values) != 1 {
			return sharedActivationRequest{}, "minimum_healthy_instances must be provided once"
		}
		minimumHealthy, err := strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil || minimumHealthy <= 0 {
			return sharedActivationRequest{}, "minimum_healthy_instances must be a positive integer"
		}
		req.minimumHealthyInstances = []int{minimumHealthy}
	}
	return req, ""
}

func firstQueryValue(query map[string][]string, key string) string {
	values := query[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (s *Server) promoteSharedGestaltState(w http.ResponseWriter, r *http.Request, req sharedActivationRequest) bool {
	if s.sourceVersion == "" {
		return true
	}
	expectedSourceVersion := strings.TrimSpace(firstQueryValue(r.URL.Query(), "source_version"))
	if expectedSourceVersion == "" {
		writeError(w, http.StatusBadRequest, "source_version is required")
		return false
	}
	if expectedSourceVersion != s.sourceVersion {
		writeError(w, http.StatusConflict, "source_version does not match this gestaltd revision")
		return false
	}
	if s.gestaltdSourceVersions == nil {
		writeError(w, http.StatusServiceUnavailable, "gestaltd source version service is unavailable")
		return false
	}
	_, err := s.gestaltdSourceVersions.ActivateWithRolloutMode(
		r.Context(),
		s.sourceVersion,
		s.now(),
		req.retry,
		appregistry.DefaultRolloutEnrollmentWindow,
		appregistry.DefaultRolloutTimeout,
		core.AppRolloutMode(s.appRegistryRolloutMode),
		req.minimumHealthyInstances...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if s.finishSharedStartupPromotion != nil {
		if err := s.finishSharedStartupPromotion(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return false
		}
	}
	return true
}

func (s *Server) runLocalActivation(w http.ResponseWriter, r *http.Request) bool {
	if s.activateAppProviders != nil {
		s.activateAppProviders(r.Context())
	}
	if s.waitAppProvidersReady != nil {
		if err := s.waitAppProvidersReady(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return false
		}
	}
	if err := s.waitForServingReady(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return false
	}
	return true
}
