package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/valon-technologies/gestalt/server/internal/coredata"
)

const (
	managedSecretDefaultAuditLimit = 50
	managedSecretMaxAuditLimit     = 200
	managedSecretMaxPlaintextBytes = 64 * 1024
)

type adminManagedSecretSummary struct {
	Name          string  `json:"name"`
	OwnerApp      string  `json:"ownerApp"`
	Scope         string  `json:"scope"`
	Description   string  `json:"description"`
	CurrentVer    int64   `json:"currentVersion"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
	CreatedBy     string  `json:"createdBy"`
	UpdatedBy     string  `json:"updatedBy"`
	RetiredAt     *string `json:"retiredAt,omitempty"`
	RetiredReason string  `json:"retiredReason,omitempty"`
}

type adminManagedSecretDetail struct {
	adminManagedSecretSummary
	Audit []adminManagedSecretAuditRow `json:"audit"`
}

type adminManagedSecretAuditRow struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Version   int64  `json:"version"`
	Actor     string `json:"actor"`
	Reason    string `json:"reason"`
	Source    string `json:"source,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	KMSKey    string `json:"kmsKey,omitempty"`
	CreatedAt string `json:"createdAt"`
	Result    string `json:"result"`
}

type adminManagedSecretWriteRequest struct {
	Name           string `json:"name"`
	Operation      string `json:"operation"`
	OwnerApp       string `json:"ownerApp"`
	Scope          string `json:"scope"`
	Description    string `json:"description"`
	Reason         string `json:"reason"`
	Source         string `json:"source"`
	RequestID      string `json:"requestId"`
	Value          string `json:"value"`
	Generate       *bool  `json:"generate,omitempty"`
	generatedValue string
}

type adminManagedSecretWriteResponse struct {
	Secret          adminManagedSecretSummary      `json:"secret"`
	Version         adminManagedSecretAuditRowLike `json:"version"`
	GeneratedValue  string                         `json:"generatedValue,omitempty"`
	RolloutRequired bool                           `json:"rolloutRequired"`
}

// adminManagedSecretAuditRowLike intentionally omits ciphertext and its hash.
// Clients need version identity and provenance, not ciphertext material.
type adminManagedSecretAuditRowLike struct {
	Version   int64  `json:"version"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
	CreatedBy string `json:"createdBy"`
	Reason    string `json:"reason"`
	KMSKey    string `json:"kmsKey,omitempty"`
}

type adminManagedSecretRetireRequest struct {
	Reason string `json:"reason"`
}

type adminManagedSecretReference struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	App      string `json:"app,omitempty"`
	Field    string `json:"field,omitempty"`
}

type adminManagedSecretPreflightResponse struct {
	OK           bool                          `json:"ok"`
	Missing      []adminManagedSecretReference `json:"missing"`
	Unreferenced []adminManagedSecretReference `json:"unreferenced"`
}

func (s *Server) mountAdminManagedSecretRoutes(r chi.Router) {
	r.Get("/managed-secrets", s.listAdminManagedSecrets)
	r.Post("/managed-secrets", s.writeAdminManagedSecret)
	r.Get("/managed-secrets/{name}", s.getAdminManagedSecret)
	r.Get("/managed-secrets/{name}/audit", s.listAdminManagedSecretAudit)
	r.Post("/managed-secrets/{name}/retire", s.retireAdminManagedSecret)
	r.Post("/managed-secrets/preflight", s.adminManagedSecretPreflight)
}

func (s *Server) managedSecretService() *coredata.ManagedSecretService {
	return s.managedSecrets
}

func (s *Server) listAdminManagedSecrets(w http.ResponseWriter, r *http.Request) {
	service := s.managedSecretService()
	if service == nil {
		writeError(w, http.StatusServiceUnavailable, "managed secrets service is unavailable")
		return
	}
	secrets, err := service.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list managed secrets")
		return
	}
	out := make([]adminManagedSecretSummary, 0, len(secrets))
	for _, secret := range secrets {
		out = append(out, adminManagedSecretSummaryFromCore(secret))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAdminManagedSecret(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if err := coredata.ValidateSecretName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	service := s.managedSecretService()
	if service == nil {
		writeError(w, http.StatusServiceUnavailable, "managed secrets service is unavailable")
		return
	}
	secret, err := service.Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, coredata.ErrSecretNotFound) {
			writeError(w, http.StatusNotFound, "managed secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load managed secret")
		return
	}
	auditRows, err := service.ListAudit(r.Context(), name, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load managed secret audit")
		return
	}
	audit := make([]adminManagedSecretAuditRow, 0, len(auditRows))
	for _, row := range auditRows {
		audit = append(audit, adminManagedSecretAuditRowFromCore(row))
	}
	writeJSON(w, http.StatusOK, adminManagedSecretDetail{
		adminManagedSecretSummaryFromCore(secret),
		audit,
	})
}

func (s *Server) listAdminManagedSecretAudit(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if err := coredata.ValidateSecretName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	service := s.managedSecretService()
	if service == nil {
		writeError(w, http.StatusServiceUnavailable, "managed secrets service is unavailable")
		return
	}
	limit := parseManagedSecretAuditLimit(w, r)
	if limit < 0 {
		return
	}
	rows, err := service.ListAudit(r.Context(), name, limit)
	if err != nil {
		if errors.Is(err, coredata.ErrSecretNotFound) {
			writeError(w, http.StatusNotFound, "managed secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load managed secret audit")
		return
	}
	out := make([]adminManagedSecretAuditRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminManagedSecretAuditRowFromCore(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) writeAdminManagedSecret(w http.ResponseWriter, r *http.Request) {
	service := s.managedSecretService()
	cipher := s.managedSecretCipher
	if service == nil || cipher == nil {
		writeError(w, http.StatusServiceUnavailable, managedSecretUnavailableMessage)
		return
	}
	var req adminManagedSecretWriteRequest
	if err := decodeManagedSecretRequest(w, r, &req); err != nil {
		return
	}
	name := strings.TrimSpace(req.Name)
	if err := coredata.ValidateSecretName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	operation := coredata.ManagedSecretOperation(strings.TrimSpace(req.Operation))
	if operation != coredata.ManagedSecretCreate && operation != coredata.ManagedSecretRotate {
		writeError(w, http.StatusBadRequest, "operation must be create or rotate")
		return
	}
	actor, ok := s.managedSecretActor(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}

	// Fetch existing metadata before generating a value. Generate must happen
	// before the transactional Put, and idempotent retries need the original
	// generated value only if the platform later chooses to store it securely.
	existing, getErr := service.Get(r.Context(), name)
	if getErr != nil && !errors.Is(getErr, coredata.ErrSecretNotFound) {
		writeError(w, http.StatusInternalServerError, "failed to load managed secret")
		return
	}
	if existing == nil && operation == coredata.ManagedSecretRotate {
		writeError(w, http.StatusNotFound, "managed secret not found")
		return
	}
	if existing != nil && operation == coredata.ManagedSecretCreate {
		writeError(w, http.StatusConflict, "managed secret already exists")
		return
	}
	if existing != nil && existing.RetiredAt != nil {
		writeError(w, http.StatusBadRequest, "cannot write retired secret")
		return
	}

	generate := req.Generate != nil && *req.Generate
	value := []byte(req.Value)
	if generate {
		if req.Value != "" {
			writeError(w, http.StatusBadRequest, "value and generate are mutually exclusive")
			return
		}
		generated, raw, err := coredata.GenerateSecretValue(48)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate secret value")
			return
		}
		value = raw
		req.generatedValue = generated
	}
	if len(value) == 0 {
		writeError(w, http.StatusBadRequest, "secret value is required")
		return
	}
	if len(value) > managedSecretMaxPlaintextBytes {
		writeError(w, http.StatusBadRequest, "secret value exceeds 64 KiB")
		return
	}

	encrypted, err := cipher.Encrypt(r.Context(), name, value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt secret value")
		return
	}
	secret, version, err := service.Put(r.Context(), coredata.PutSecretInput{
		Operation:     operation,
		Name:          name,
		OwnerApp:      req.OwnerApp,
		Scope:         req.Scope,
		Description:   req.Description,
		Reason:        req.Reason,
		Actor:         actor,
		Source:        req.Source,
		RequestID:     req.RequestID,
		KMSKey:        encrypted.KMSKey,
		KMSKeyVersion: encrypted.KMSKeyVersion,
		Ciphertext:    encrypted.Ciphertext,
	})
	if err != nil {
		if errors.Is(err, coredata.ErrSecretExists) {
			writeError(w, http.StatusConflict, "managed secret already exists")
			return
		}
		if errors.Is(err, coredata.ErrSecretNotFound) {
			writeError(w, http.StatusNotFound, "managed secret not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, adminManagedSecretWriteResponse{
		Secret: adminManagedSecretSummaryFromCore(secret),
		Version: adminManagedSecretAuditRowLike{
			Version:   version.Version,
			State:     version.State,
			CreatedAt: formatAdminTime(version.CreatedAt),
			CreatedBy: version.CreatedBy,
			Reason:    version.Reason,
			KMSKey:    version.KMSKey,
		},
		GeneratedValue:  req.generatedValue,
		RolloutRequired: secret.CurrentVer > 1,
	})
}

func (s *Server) retireAdminManagedSecret(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if err := coredata.ValidateSecretName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	service := s.managedSecretService()
	if service == nil {
		writeError(w, http.StatusServiceUnavailable, "managed secrets service is unavailable")
		return
	}
	var req adminManagedSecretRetireRequest
	if err := decodeManagedSecretRequest(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	actor, ok := s.managedSecretActor(w, r)
	if !ok {
		return
	}
	secret, err := service.Retire(r.Context(), name, actor, req.Reason, s.now())
	if err != nil {
		if errors.Is(err, coredata.ErrSecretNotFound) {
			writeError(w, http.StatusNotFound, "managed secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to retire managed secret")
		return
	}
	writeJSON(w, http.StatusOK, adminManagedSecretSummaryFromCore(secret))
}

func (s *Server) adminManagedSecretPreflight(w http.ResponseWriter, r *http.Request) {
	service := s.managedSecretService()
	if service == nil {
		writeError(w, http.StatusServiceUnavailable, "managed secrets service is unavailable")
		return
	}
	var refs []adminManagedSecretReference
	if err := decodeManagedSecretRequest(w, r, &refs); err != nil {
		return
	}
	existing, err := service.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list managed secrets")
		return
	}
	existingNames := make(map[string]*coredata.ManagedSecret, len(existing))
	for _, secret := range existing {
		existingNames[secret.Name] = secret
	}
	referenced := make(map[string]struct{}, len(refs))
	missing := make([]adminManagedSecretReference, 0)
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			continue
		}
		referenced[name] = struct{}{}
		secret, ok := existingNames[name]
		if !ok {
			missing = append(missing, ref)
			continue
		}
		// Only the relational secrets provider participates in this store.
		if provider := strings.TrimSpace(ref.Provider); provider != "" && provider != "secrets" {
			missing = append(missing, ref)
			continue
		}
		if app := strings.TrimSpace(ref.App); app != "" && secret.Scope == "app" && secret.OwnerApp != app {
			missing = append(missing, ref)
		}
	}
	unreferenced := make([]adminManagedSecretReference, 0)
	for _, secret := range existing {
		if _, ok := referenced[secret.Name]; !ok {
			unreferenced = append(unreferenced, adminManagedSecretReference{
				Name:     secret.Name,
				Provider: "secrets",
			})
		}
	}
	writeJSON(w, http.StatusOK, adminManagedSecretPreflightResponse{
		OK:           len(missing) == 0,
		Missing:      missing,
		Unreferenced: unreferenced,
	})
}

const managedSecretUnavailableMessage = "managed secrets service or encryption is unavailable"

func decodeManagedSecretRequest(w http.ResponseWriter, r *http.Request, target any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return err
	}
	return nil
}

func parseManagedSecretAuditLimit(w http.ResponseWriter, r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return managedSecretDefaultAuditLimit
	}
	limit := 0
	if _, err := fmtSscan(raw, &limit); err != nil || limit <= 0 {
		writeError(w, http.StatusBadRequest, "limit must be a positive integer")
		return -1
	}
	if limit > managedSecretMaxAuditLimit {
		limit = managedSecretMaxAuditLimit
	}
	return limit
}

func fmtSscan(raw string, target *int) (int, error) {
	return fmt.Sscan(raw, target)
}

func (s *Server) managedSecretActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := PrincipalFromContext(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "authenticated user required")
		return "", false
	}
	if subject := strings.TrimSpace(p.UserID); subject != "" {
		return subject, true
	}
	if p.Identity != nil {
		if subject := strings.TrimSpace(p.Identity.Email); subject != "" {
			return subject, true
		}
	}
	writeError(w, http.StatusUnauthorized, "authenticated actor is required")
	return "", false
}

func adminManagedSecretSummaryFromCore(secret *coredata.ManagedSecret) adminManagedSecretSummary {
	var retiredAt *string
	if secret.RetiredAt != nil {
		formatted := formatAdminTime(*secret.RetiredAt)
		retiredAt = &formatted
	}
	return adminManagedSecretSummary{
		Name:          secret.Name,
		OwnerApp:      secret.OwnerApp,
		Scope:         secret.Scope,
		Description:   secret.Description,
		CurrentVer:    secret.CurrentVer,
		CreatedAt:     formatAdminTime(secret.CreatedAt),
		UpdatedAt:     formatAdminTime(secret.UpdatedAt),
		CreatedBy:     secret.CreatedBy,
		UpdatedBy:     secret.UpdatedBy,
		RetiredAt:     retiredAt,
		RetiredReason: secret.RetiredReason,
	}
}

func adminManagedSecretAuditRowFromCore(row *coredata.ManagedSecretAuditLog) adminManagedSecretAuditRow {
	return adminManagedSecretAuditRow{
		ID:        row.ID,
		Action:    row.Action,
		Version:   row.Version,
		Actor:     row.Actor,
		Reason:    row.Reason,
		Source:    row.Source,
		RequestID: row.RequestID,
		KMSKey:    row.KMSKey,
		CreatedAt: formatAdminTime(row.CreatedAt),
		Result:    row.Result,
	}
}

// The following aliases let package server_test construct expected payloads
// without duplicating response shapes. They intentionally expose no secret
// material.
type (
	// ManagedSecretSummaryAlias is the public test alias for a secret summary.
	ManagedSecretSummaryAlias = adminManagedSecretSummary
	// ManagedSecretDetailAlias is the public test alias for secret detail.
	ManagedSecretDetailAlias = adminManagedSecretDetail
	// ManagedSecretAuditRowAlias is the public test alias for audit rows.
	ManagedSecretAuditRowAlias = adminManagedSecretAuditRow
	// ManagedSecretWriteResponseAlias is the public test alias for writes.
	ManagedSecretWriteResponseAlias = adminManagedSecretWriteResponse
	// ManagedSecretReferenceAlias is the public test alias for references.
	ManagedSecretReferenceAlias = adminManagedSecretReference
	// ManagedSecretPreflightResponseAlias is the public test alias for preflight.
	ManagedSecretPreflightResponseAlias = adminManagedSecretPreflightResponse
)
