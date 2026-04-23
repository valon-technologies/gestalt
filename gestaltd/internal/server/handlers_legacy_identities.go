package server

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

type legacyManagedIdentityInfo struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (s *Server) listLegacyManagedIdentities(w http.ResponseWriter, r *http.Request) {
	managerIdentityID := legacyManagedIdentityManagerID(PrincipalFromContext(r.Context()))
	if managerIdentityID == "" || s.identities == nil || s.identityGrants == nil {
		writeJSON(w, http.StatusOK, []legacyManagedIdentityInfo{})
		return
	}

	grants, err := s.identityGrants.ListByManager(r.Context(), managerIdentityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list identities")
		return
	}

	now := s.nowUTCSecond()
	out := make([]legacyManagedIdentityInfo, 0, len(grants))
	seen := make(map[string]struct{}, len(grants))
	for _, grant := range grants {
		if grant == nil || strings.TrimSpace(grant.TargetIdentityID) == "" {
			continue
		}
		if grant.ExpiresAt != nil && !grant.ExpiresAt.After(now) {
			continue
		}
		if _, ok := seen[grant.TargetIdentityID]; ok {
			continue
		}
		seen[grant.TargetIdentityID] = struct{}{}

		identity, err := s.identities.GetIdentity(r.Context(), grant.TargetIdentityID)
		switch {
		case err == nil:
		case errors.Is(err, core.ErrNotFound):
			continue
		default:
			writeError(w, http.StatusInternalServerError, "failed to list identities")
			return
		}
		out = append(out, legacyManagedIdentityInfo{
			ID:          identity.ID,
			DisplayName: identity.DisplayName,
			Role:        strings.TrimSpace(grant.Role),
			CreatedAt:   identity.CreatedAt,
			UpdatedAt:   identity.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].ID < out[j].ID
	})

	writeJSON(w, http.StatusOK, out)
}

func legacyManagedIdentityManagerID(p *principal.Principal) string {
	p = principal.Canonicalized(p)
	if p == nil || principal.IsWorkloadPrincipal(p) {
		return ""
	}
	return strings.TrimSpace(p.UserID)
}

func (s *Server) legacyManagedIdentitiesGone(w http.ResponseWriter, _ *http.Request) {
	writeTypedError(w, http.StatusGone, "managed_identities_removed", "", "managed identities have been removed; use canonical identity authorization instead")
}
