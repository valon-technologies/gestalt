package providertest

import (
	"sort"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/provider"
)

type DefinitionExpect struct {
	Name           string
	OperationCount int
	AuthType       string
	ConnectionMode string
	Connection     map[string]ConnParam
}

type ConnParam struct {
	Required bool
	From     string
}

type ProviderExpect struct {
	Name           string
	ConnectionMode core.ConnectionMode
	OperationCount int
	OperationNames []string
}

type DiscoveryExpect struct {
	URL             string
	MetadataMapping map[string]string
}

func ParseDefinition(t *testing.T, yamlData []byte) *provider.Definition {
	t.Helper()
	var def provider.Definition
	if err := yaml.Unmarshal(yamlData, &def); err != nil {
		t.Fatalf("unmarshal provider definition: %v", err)
	}
	return &def
}

func CheckDefinition(t *testing.T, def *provider.Definition, expect DefinitionExpect) {
	t.Helper()

	if def.Provider != expect.Name {
		t.Fatalf("provider = %q, want %q", def.Provider, expect.Name)
	}
	if len(def.Operations) != expect.OperationCount {
		t.Fatalf("operations = %d, want %d", len(def.Operations), expect.OperationCount)
	}
	if expect.AuthType != "" && def.Auth.Type != expect.AuthType {
		t.Fatalf("auth.type = %q, want %q", def.Auth.Type, expect.AuthType)
	}
	if expect.ConnectionMode != "" && def.ConnectionMode != expect.ConnectionMode {
		t.Fatalf("connection_mode = %q, want %q", def.ConnectionMode, expect.ConnectionMode)
	}
	for name, expected := range expect.Connection {
		cp, ok := def.Connection[name]
		if !ok {
			t.Fatalf("missing connection param %q", name)
		}
		if cp.Required != expected.Required {
			t.Errorf("connection[%q].required = %v, want %v", name, cp.Required, expected.Required)
		}
		if cp.From != expected.From {
			t.Errorf("connection[%q].from = %q, want %q", name, cp.From, expected.From)
		}
	}
}

func BuildProvider(t *testing.T, def *provider.Definition, conn config.ConnectionDef, opts ...provider.BuildOption) core.Provider {
	t.Helper()
	prov, err := provider.Build(def, conn, nil, opts...)
	if err != nil {
		t.Fatalf("provider.Build: %v", err)
	}
	return prov
}

func CheckProvider(t *testing.T, prov core.Provider, expect ProviderExpect) {
	t.Helper()

	if prov.Name() != expect.Name {
		t.Fatalf("Name() = %q, want %q", prov.Name(), expect.Name)
	}
	if prov.ConnectionMode() != expect.ConnectionMode {
		t.Fatalf("ConnectionMode() = %q, want %q", prov.ConnectionMode(), expect.ConnectionMode)
	}

	ops := prov.ListOperations()

	wantCount := expect.OperationCount
	if expect.OperationNames != nil {
		wantCount = len(expect.OperationNames)
	}
	if len(ops) != wantCount {
		t.Fatalf("ListOperations() = %d, want %d", len(ops), wantCount)
	}

	if expect.OperationNames != nil {
		got := make([]string, len(ops))
		for i, op := range ops {
			got[i] = op.Name
		}
		sort.Strings(got)

		want := make([]string, len(expect.OperationNames))
		copy(want, expect.OperationNames)
		sort.Strings(want)

		for i := range got {
			if got[i] != want[i] {
				t.Errorf("operation mismatch at sorted index %d: got %q, want %q", i, got[i], want[i])
			}
		}
	}
}

func CheckDiscovery(t *testing.T, prov core.Provider, expect DiscoveryExpect) {
	t.Helper()
	dcp, ok := prov.(core.DiscoveryConfigProvider)
	if !ok {
		t.Fatal("provider does not implement DiscoveryConfigProvider")
	}
	dc := dcp.DiscoveryConfig()
	if dc == nil {
		t.Fatal("DiscoveryConfig() returned nil")
	}
	if dc.URL != expect.URL {
		t.Fatalf("DiscoveryConfig().URL = %q, want %q", dc.URL, expect.URL)
	}
	for key, wantVal := range expect.MetadataMapping {
		gotVal, ok := dc.MetadataMapping[key]
		if !ok {
			t.Errorf("DiscoveryConfig().MetadataMapping missing key %q", key)
		} else if gotVal != wantVal {
			t.Errorf("DiscoveryConfig().MetadataMapping[%q] = %q, want %q", key, gotVal, wantVal)
		}
	}
}

func CheckConnectionParams(t *testing.T, prov core.Provider, expected map[string]ConnParam) {
	t.Helper()
	cpp, ok := prov.(core.ConnectionParamProvider)
	if !ok {
		t.Fatal("provider does not implement ConnectionParamProvider")
	}
	defs := cpp.ConnectionParamDefs()
	for name, expect := range expected {
		cpd, ok := defs[name]
		if !ok {
			t.Errorf("missing connection param %q", name)
			continue
		}
		if cpd.Required != expect.Required {
			t.Errorf("connection param %q: required = %v, want %v", name, cpd.Required, expect.Required)
		}
		if cpd.From != expect.From {
			t.Errorf("connection param %q: from = %q, want %q", name, cpd.From, expect.From)
		}
	}
}

func CheckOAuth(t *testing.T, prov core.Provider) {
	t.Helper()
	if _, ok := prov.(core.OAuthProvider); !ok {
		t.Fatal("provider does not implement OAuthProvider")
	}
}

func CheckManualAuth(t *testing.T, prov core.Provider) {
	t.Helper()
	mp, ok := prov.(core.ManualProvider)
	if !ok {
		t.Fatal("provider does not implement ManualProvider")
	}
	if !mp.SupportsManualAuth() {
		t.Fatal("SupportsManualAuth() = false, want true")
	}
}

func CheckCatalog(t *testing.T, prov core.Provider) {
	t.Helper()
	cp, ok := prov.(core.CatalogProvider)
	if !ok {
		t.Fatal("provider does not implement CatalogProvider")
	}
	if cp.Catalog() == nil {
		t.Fatal("Catalog() returned nil")
	}
}
