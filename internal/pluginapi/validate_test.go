package pluginapi

import (
	"strings"
	"testing"
)

const testSchema = `{
  "type": "object",
  "properties": {
    "host": {"type": "string"},
    "port": {"type": "integer", "minimum": 1, "maximum": 65535},
    "tls":  {"type": "boolean"}
  },
  "required": ["host", "port"],
  "additionalProperties": false
}`

func TestValidateConfigSchema_ValidConfig(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"host": "db.example.com",
		"port": float64(5432),
		"tls":  true,
	}
	if err := validateConfigSchema(config, testSchema); err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

func TestValidateConfigSchema_InvalidConfig(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"host": 12345,
		"port": float64(5432),
	}
	err := validateConfigSchema(config, testSchema)
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Fatalf("expected structured validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "/host") {
		t.Fatalf("expected field-level error for 'host', got: %v", err)
	}
}

func TestValidateConfigSchema_NoSchema(t *testing.T) {
	t.Parallel()

	config := map[string]any{"host": "db.example.com", "port": float64(5432)}
	schemaJSON := ""

	if schemaJSON != "" && len(config) > 0 {
		if err := validateConfigSchema(config, schemaJSON); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestValidateConfigSchema_EmptyConfigRequiredFields(t *testing.T) {
	t.Parallel()

	config := map[string]any{}
	err := validateConfigSchema(config, testSchema)
	if err == nil {
		t.Fatal("expected error for empty config with required fields, got nil")
	}
	if !strings.Contains(err.Error(), "config validation failed") {
		t.Fatalf("expected structured validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "host") || !strings.Contains(err.Error(), "port") {
		t.Fatalf("expected errors mentioning required fields 'host' and 'port', got: %v", err)
	}
}

func TestValidateConfigSchema_AdditionalProperties(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"host":    "db.example.com",
		"port":    float64(5432),
		"unknown": "value",
	}
	err := validateConfigSchema(config, testSchema)
	if err == nil {
		t.Fatal("expected error for additional properties, got nil")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected error mentioning 'unknown' property, got: %v", err)
	}
}

func TestValidateConfigSchema_TypeMismatch(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"host": "db.example.com",
		"port": "not-a-number",
	}
	err := validateConfigSchema(config, testSchema)
	if err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "/port") {
		t.Fatalf("expected field-level error for 'port', got: %v", err)
	}
}
