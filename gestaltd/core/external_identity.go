package core

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// ExternalIdentityRef identifies a provider-owned external identity resource.
// The Type names the provider namespace; the ID is opaque to Gestalt after any
// config surface has rendered its own templates.
type ExternalIdentityRef struct {
	Type string
	ID   string
}

func NormalizeExternalIdentityRef(ref *ExternalIdentityRef) *ExternalIdentityRef {
	if ref == nil {
		return nil
	}
	out := &ExternalIdentityRef{
		Type: strings.TrimSpace(ref.Type),
		ID:   strings.TrimSpace(ref.ID),
	}
	if out.Type == "" || out.ID == "" {
		return nil
	}
	return out
}

func ExternalIdentityRefsEqual(left, right *ExternalIdentityRef) bool {
	left = NormalizeExternalIdentityRef(left)
	right = NormalizeExternalIdentityRef(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Type == right.Type && left.ID == right.ID
}

func ExternalIdentityResourceID(ref *ExternalIdentityRef) string {
	ref = NormalizeExternalIdentityRef(ref)
	if ref == nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(ref.Type + "\x00" + ref.ID))
}

func ExternalIdentityRefFromResourceID(resourceID string) (*ExternalIdentityRef, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, fmt.Errorf("external identity resource id is required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(resourceID)
	if err != nil {
		return nil, fmt.Errorf("decode external identity resource id: %w", err)
	}
	identityType, identityID, ok := strings.Cut(string(raw), "\x00")
	if !ok {
		return nil, fmt.Errorf("external identity resource id is malformed")
	}
	ref := NormalizeExternalIdentityRef(&ExternalIdentityRef{Type: identityType, ID: identityID})
	if ref == nil {
		return nil, fmt.Errorf("external identity resource id is malformed")
	}
	return ref, nil
}
