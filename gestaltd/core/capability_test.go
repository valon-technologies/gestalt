package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCapabilityAnnotationsJSONPreservesLegacyEncoding(t *testing.T) {
	t.Parallel()

	readOnly := true
	data, err := json.Marshal(CapabilityAnnotations{ReadOnlyHint: &readOnly})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	encoded := string(data)
	if !strings.Contains(encoded, `"ReadOnlyHint":true`) {
		t.Fatalf("encoded = %s, want PascalCase ReadOnlyHint key", encoded)
	}
	if strings.Contains(encoded, "readOnlyHint") {
		t.Fatalf("encoded = %s, must not use catalog camelCase json tags", encoded)
	}
}
