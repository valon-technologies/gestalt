package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/authorization"
)

const (
	connectionMetadataExternalIdentityTypeKey = "gestalt.external_identity.type"
	connectionMetadataExternalIdentityIDKey   = "gestalt.external_identity.id"
	externalIdentityLinkRelation              = authorization.ProviderExternalIdentityRelationAssume
)

type externalIdentityRef struct {
	Type string
	ID   string
}

func normalizeExternalIdentityRef(ref externalIdentityRef) externalIdentityRef {
	return externalIdentityRef{
		Type: strings.TrimSpace(ref.Type),
		ID:   strings.TrimSpace(ref.ID),
	}
}

func externalIdentityRefsEqual(a, b externalIdentityRef) bool {
	return normalizeExternalIdentityRef(a) == normalizeExternalIdentityRef(b)
}

func validateExternalIdentityRef(ref externalIdentityRef) error {
	ref = normalizeExternalIdentityRef(ref)
	if ref.Type == "" {
		return fmt.Errorf("external identity type is required")
	}
	if ref.ID == "" {
		return fmt.Errorf("external identity id is required")
	}
	if !safeParamValue.MatchString(ref.Type) {
		return fmt.Errorf("external identity type %q contains invalid characters", ref.Type)
	}
	if !safeTokenResponseValue.MatchString(ref.ID) {
		return fmt.Errorf("external identity id %q contains invalid characters", ref.ID)
	}
	return nil
}

func externalIdentityResourceID(ref externalIdentityRef) string {
	ref = normalizeExternalIdentityRef(ref)
	if ref.Type == "" || ref.ID == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(ref.Type + "\x00" + ref.ID))
}

func externalIdentityRefFromMetadataJSON(metadataJSON string) (externalIdentityRef, bool, error) {
	metadataJSON = strings.TrimSpace(metadataJSON)
	if metadataJSON == "" {
		return externalIdentityRef{}, false, nil
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return externalIdentityRef{}, false, fmt.Errorf("parse connection metadata: %w", err)
	}
	return externalIdentityRefFromMetadata(metadata)
}

func externalIdentityRefFromMetadata(metadata map[string]string) (externalIdentityRef, bool, error) {
	if len(metadata) == 0 {
		return externalIdentityRef{}, false, nil
	}
	ref := normalizeExternalIdentityRef(externalIdentityRef{
		Type: metadata[connectionMetadataExternalIdentityTypeKey],
		ID:   metadata[connectionMetadataExternalIdentityIDKey],
	})
	if ref.Type == "" && ref.ID == "" {
		return externalIdentityRef{}, false, nil
	}
	if err := validateExternalIdentityRef(ref); err != nil {
		return externalIdentityRef{}, false, err
	}
	return ref, true, nil
}
