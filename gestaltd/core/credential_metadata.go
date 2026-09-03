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

// AccountKeyMetadataKey is the reserved ExternalCredential.MetadataJSON key
// for the host-owned stable identity of the linked provider account. It is
// intentionally separate from AccountIdentityMetadataKey, which contains
// display-oriented recognition facts.
const AccountKeyMetadataKey = "account_key"

// AccountKeyFromMetadataJSON returns the explicitly stored provider-owned
// account key. It deliberately does not infer a key from display identity
// facts, because those facts are not strong enough to prove account equality.
func AccountKeyFromMetadataJSON(metadataJSON string) string {
	if strings.TrimSpace(metadataJSON) == "" {
		return ""
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return ""
	}
	return strings.TrimSpace(metadata[AccountKeyMetadataKey])
}

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
	delete(params, AccountKeyMetadataKey)
	if len(params) == 0 {
		return nil, nil
	}
	return params, nil
}
