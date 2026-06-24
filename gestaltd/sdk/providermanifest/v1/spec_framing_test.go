package providermanifestv1_test

import (
	"encoding/json"
	"testing"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"gopkg.in/yaml.v3"
)

func TestSpecAllowSameOriginFramingYAML(t *testing.T) {
	t.Parallel()

	var spec providermanifestv1.Spec
	if err := yaml.Unmarshal([]byte("assetRoot: out\nallowSameOriginFraming: true\n"), &spec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !spec.AllowSameOriginFraming {
		t.Fatalf("AllowSameOriginFraming = false; want true")
	}

	var omitted providermanifestv1.Spec
	if err := yaml.Unmarshal([]byte("assetRoot: out\n"), &omitted); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if omitted.AllowSameOriginFraming {
		t.Fatalf("AllowSameOriginFraming = true; want false when omitted")
	}
}

func TestSpecAllowSameOriginFramingRoundTrip(t *testing.T) {
	t.Parallel()

	spec := providermanifestv1.Spec{AssetRoot: "out", AllowSameOriginFraming: true}

	yamlOut, err := yaml.Marshal(spec)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	var fromYAML providermanifestv1.Spec
	if err := yaml.Unmarshal(yamlOut, &fromYAML); err != nil {
		t.Fatalf("yaml round-trip Unmarshal: %v", err)
	}
	if !fromYAML.AllowSameOriginFraming {
		t.Fatalf("yaml round-trip lost AllowSameOriginFraming")
	}

	jsonOut, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var fromJSON providermanifestv1.Spec
	if err := json.Unmarshal(jsonOut, &fromJSON); err != nil {
		t.Fatalf("json round-trip Unmarshal: %v", err)
	}
	if !fromJSON.AllowSameOriginFraming {
		t.Fatalf("json round-trip lost AllowSameOriginFraming")
	}
}
