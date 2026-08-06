package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AccountIdentityMetadataKey is the reserved ExternalCredential.MetadataJSON
// key for Connection account recognition facts. It must not be treated as a
// runtime connection parameter (URL/header interpolation, provider params).
const AccountIdentityMetadataKey = "account_identity"

// ConnectionParamsFromMetadataJSON unmarshals credential MetadataJSON into
// connection params for runtime use, stripping host-owned reserved keys.
func ConnectionParamsFromMetadataJSON(metadataJSON string) (map[string]string, error) {
	if strings.TrimSpace(metadataJSON) == "" {
		return nil, nil
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(metadataJSON), &params); err != nil {
		return nil, fmt.Errorf("corrupt MetadataJSON: %w", err)
	}
	if len(params) == 0 {
		return nil, nil
	}
	delete(params, AccountIdentityMetadataKey)
	if len(params) == 0 {
		return nil, nil
	}
	return params, nil
}
