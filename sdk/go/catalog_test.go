package gestalt

import (
	"encoding/json"
	"testing"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func TestAPIExposureModeJSONCompatibility(t *testing.T) {
	for _, test := range []struct {
		name string
		json string
		want APIExposureMode
	}{
		{name: "public", json: "true", want: APIExposurePublic},
		{name: "private", json: "false", want: APIExposurePrivate},
		{name: "browser session", json: `"browserSession"`, want: APIExposureBrowserSession},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got APIExposureMode
			if err := json.Unmarshal([]byte(test.json), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(encoded) != test.json {
				t.Fatalf("encoded %s, want %s", encoded, test.json)
			}
		})
	}

	var unknown APIExposureMode
	if err := json.Unmarshal([]byte(`"futureMode"`), &unknown); err == nil {
		t.Fatal("unknown API exposure mode was accepted")
	}
	if err := json.Unmarshal([]byte("null"), &unknown); err == nil {
		t.Fatal("null API exposure mode was accepted")
	}
}

func TestCatalogOperationBrowserSessionDowngradeWire(t *testing.T) {
	mode := APIExposureBrowserSession
	converted, err := catalogOperationToProto(&CatalogOperation{API: &mode})
	if err != nil {
		t.Fatalf("convert catalog operation: %v", err)
	}
	if converted.Api == nil || *converted.Api {
		t.Fatalf("browser session must set legacy api=false, got %v", converted.Api)
	}
	if converted.ApiMode == nil || *converted.ApiMode != proto.APIExposureMode_API_EXPOSURE_MODE_BROWSER_SESSION {
		t.Fatalf("browser session api_mode missing: %v", converted.ApiMode)
	}
}
