package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateBuiltinAuditRejectsUnknownOTLPProtocol(t *testing.T) {
	t.Parallel()

	entry := &ProviderEntry{
		Source: ProviderSource{Builtin: "otlp"},
		Config: mustConfigNode(t, "protocol: ftp\n"),
	}

	err := validateBuiltinAudit(entry)
	if err == nil {
		t.Fatal("validateBuiltinAudit() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "otlp audit: parsing config") {
		t.Fatalf("validateBuiltinAudit() error = %q, want parsing config prefix", err)
	}
	if !strings.Contains(err.Error(), "unknown protocol") {
		t.Fatalf("validateBuiltinAudit() error = %q, want unknown protocol detail", err)
	}
}

func TestValidateBuiltinAuditRejectsNonOTLPLogsExporter(t *testing.T) {
	t.Parallel()

	entry := &ProviderEntry{
		Source: ProviderSource{Builtin: "otlp"},
		Config: mustConfigNode(t, "logs:\n  exporter: stdout\n"),
	}

	err := validateBuiltinAudit(entry)
	if err == nil {
		t.Fatal("validateBuiltinAudit() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `logs.exporter must be "otlp"`) {
		t.Fatalf("validateBuiltinAudit() error = %q, want exporter requirement", err)
	}
}

func mustConfigNode(t *testing.T, content string) yaml.Node {
	t.Helper()

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("yaml.Unmarshal(): %v", err)
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		return *doc.Content[0]
	}
	return doc
}
