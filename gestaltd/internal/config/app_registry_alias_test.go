package config

import "testing"

func TestRegistryAppAliasSurvivesConfigLoadAndMarshal(t *testing.T) {
	path := mustWriteConfigFile(t, `
apiVersion: gestaltd.config/v8
appRegistries:
  toolshed:
    kind: gcs
    gcs:
      bucket: gs://test-app-registry
apps:
  ciWorkqueue:
    source:
      registry: toolshed
      registryApp: ci-workqueue
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	source := cfg.Apps["ciWorkqueue"].Source
	if got := source.RegistryAppName("ciWorkqueue"); got != "ci-workqueue" {
		t.Fatalf("RegistryAppName = %q", got)
	}
	encoded, err := source.MarshalYAML()
	if err != nil {
		t.Fatalf("MarshalYAML: %v", err)
	}
	if got := encoded.(providerSourceYAML).RegistryApp; got != "ci-workqueue" {
		t.Fatalf("marshaled registryApp = %q", got)
	}
	if got := (ProviderSource{Registry: "toolshed"}).RegistryAppName("home"); got != "home" {
		t.Fatalf("default RegistryAppName = %q", got)
	}
}
