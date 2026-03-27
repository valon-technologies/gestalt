package egress

import (
	"strings"
	"testing"
)

func TestValidateCredentialGrant_RequiresMatchCriterion(t *testing.T) {
	t.Parallel()

	err := ValidateCredentialGrant(CredentialGrantValidationInput{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "at least one match criterion") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCredentialGrant_AuthStyleOnlyIsNotSufficient(t *testing.T) {
	t.Parallel()

	err := ValidateCredentialGrant(CredentialGrantValidationInput{
		AuthStyle: "bearer",
	})
	if err == nil {
		t.Fatal("expected error when only auth_style is set")
	}
	if !strings.Contains(err.Error(), "at least one match criterion") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCredentialGrant_EachMatchCriterionSufficient(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input CredentialGrantValidationInput
	}{
		{"subject_kind", CredentialGrantValidationInput{SubjectKind: "agent"}},
		{"subject_id", CredentialGrantValidationInput{SubjectID: "agent-1"}},
		{"operation", CredentialGrantValidationInput{Operation: "chat"}},
		{"method", CredentialGrantValidationInput{Method: "POST"}},
		{"host", CredentialGrantValidationInput{Host: "api.vendor.test"}},
		{"path_prefix", CredentialGrantValidationInput{PathPrefix: "/v1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateCredentialGrant(tc.input); err != nil {
				t.Fatalf("expected no error for %s, got: %v", tc.name, err)
			}
		})
	}
}

func TestValidateCredentialGrant_AuthStyleValidation(t *testing.T) {
	t.Parallel()

	valid := []string{"bearer", "raw", "basic", "none", ""}
	for _, style := range valid {
		t.Run("valid_"+style, func(t *testing.T) {
			t.Parallel()
			err := ValidateCredentialGrant(CredentialGrantValidationInput{
				Host:      "api.vendor.test",
				AuthStyle: style,
			})
			if err != nil {
				t.Fatalf("expected no error for auth_style %q, got: %v", style, err)
			}
		})
	}

	invalid := []string{"oauth2", "BEARER", "Bearer", "token"}
	for _, style := range invalid {
		t.Run("invalid_"+style, func(t *testing.T) {
			t.Parallel()
			err := ValidateCredentialGrant(CredentialGrantValidationInput{
				Host:      "api.vendor.test",
				AuthStyle: style,
			})
			if err == nil {
				t.Fatalf("expected error for auth_style %q", style)
			}
			if !strings.Contains(err.Error(), "auth_style must be one of") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateCredentialGrant_InvalidAuthStyleWithValidCriterion(t *testing.T) {
	t.Parallel()

	err := ValidateCredentialGrant(CredentialGrantValidationInput{
		Host:      "api.vendor.test",
		AuthStyle: "oauth2",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "auth_style") {
		t.Fatalf("expected auth_style error, got: %v", err)
	}
	if strings.Contains(err.Error(), "match criterion") {
		t.Fatalf("should not report match criterion error when host is set: %v", err)
	}
}
