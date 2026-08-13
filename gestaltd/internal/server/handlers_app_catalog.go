package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) listAppCatalog(w http.ResponseWriter, r *http.Request) {
	dir, err := s.assembleAppDirectory(r)
	if err != nil {
		writeAppListingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, dir.catalogJSON())
}

func (s *Server) listAppConnections(w http.ResponseWriter, r *http.Request) {
	dir, err := s.assembleAppDirectory(r)
	if err != nil {
		writeAppListingError(w, r, err)
		return
	}
	infos, err := s.projectComposedAppListing(r, dir)
	if err != nil {
		writeAppListingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, appConnectionStatusesFrom(infos))
}

func (s *Server) serveAppCatalogIcon(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	entry, ok, err := s.visibleProviderDirectoryEntry(r, name)
	if err != nil {
		writeAppListingError(w, r, err)
		return
	}
	if !ok || strings.TrimSpace(entry.IconSVG) == "" {
		writeError(w, http.StatusNotFound, "app icon not found")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(entry.IconSVG))
}

func writeAppListingError(w http.ResponseWriter, r *http.Request, err error) {
	var listingErr *appListingError
	if errors.As(err, &listingErr) && listingErr != nil {
		slog.ErrorContext(r.Context(), "listing integrations", "error", listingErr.Unwrap())
		writeError(w, listingErr.status, listingErr.public)
		return
	}
	slog.ErrorContext(r.Context(), "listing integrations", "error", err)
	writeError(w, http.StatusInternalServerError, "failed to list apps")
}
