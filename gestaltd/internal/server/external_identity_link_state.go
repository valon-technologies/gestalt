package server

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
	"github.com/valon-technologies/gestalt/server/internal/authorization"
	"github.com/valon-technologies/gestalt/server/internal/principal"
)

const (
	externalIdentityLinkRelation = authorization.ProviderExternalIdentityRelationAssume
	externalIdentityLinkTokenTTL = 30 * time.Minute
)

type externalIdentityRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type externalIdentityLinkTokenState struct {
	ExternalIdentity externalIdentityRef `json:"externalIdentity"`
	SubjectID        string              `json:"subjectId"`
	ExpiresAt        int64               `json:"exp"`
}

func normalizeExternalIdentityRef(ref externalIdentityRef) externalIdentityRef {
	return externalIdentityRef{
		Type: strings.TrimSpace(ref.Type),
		ID:   strings.TrimSpace(ref.ID),
	}
}

func validateExternalIdentityRef(ref externalIdentityRef) error {
	ref = normalizeExternalIdentityRef(ref)
	if ref.Type == "" {
		return fmt.Errorf("external identity type is required")
	}
	if ref.ID == "" {
		return fmt.Errorf("external identity id is required")
	}
	return nil
}

func validateExternalIdentityLinkTokenState(state *externalIdentityLinkTokenState, now time.Time) error {
	if state == nil {
		return fmt.Errorf("external identity link token is required")
	}
	if err := validateExternalIdentityRef(state.ExternalIdentity); err != nil {
		return fmt.Errorf("external identity link token invalid identity: %w", err)
	}
	if strings.TrimSpace(principal.UserIDFromSubjectID(state.SubjectID)) == "" {
		return fmt.Errorf("external identity link token missing subject ID")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("external identity link token missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return fmt.Errorf("external identity link token expired")
	}
	return nil
}

func decodeExternalIdentityLinkToken(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*externalIdentityLinkTokenState, error) {
	state, err := decodeEncryptedState[externalIdentityLinkTokenState](enc, "external identity link token", encoded)
	if err != nil {
		return nil, err
	}
	if err := validateExternalIdentityLinkTokenState(state, now); err != nil {
		return nil, err
	}
	state.ExternalIdentity = normalizeExternalIdentityRef(state.ExternalIdentity)
	state.SubjectID = strings.TrimSpace(state.SubjectID)
	return state, nil
}

func externalIdentityLinkID(ref externalIdentityRef) string {
	ref = normalizeExternalIdentityRef(ref)
	if ref.Type == "" || ref.ID == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(ref.Type + "\x00" + ref.ID))
}

func decodeExternalIdentityLinkID(linkID string) (externalIdentityRef, error) {
	linkID = strings.TrimSpace(linkID)
	if linkID == "" {
		return externalIdentityRef{}, fmt.Errorf("external identity link ID is required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(linkID)
	if err != nil {
		return externalIdentityRef{}, fmt.Errorf("decode external identity link ID: %w", err)
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 {
		return externalIdentityRef{}, fmt.Errorf("external identity link ID is invalid")
	}
	ref := normalizeExternalIdentityRef(externalIdentityRef{
		Type: parts[0],
		ID:   parts[1],
	})
	if err := validateExternalIdentityRef(ref); err != nil {
		return externalIdentityRef{}, err
	}
	return ref, nil
}
