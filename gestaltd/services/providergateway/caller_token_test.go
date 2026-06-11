package providergateway

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCallerTokenIssueVerify(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	now := time.Now().UTC()
	claims, err := GenerateCallerTokenClaims("user:123", now)
	if err != nil {
		t.Fatalf("GenerateCallerTokenClaims: %v", err)
	}

	token, err := Issue(claims, secret)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, err := Verify(token, secret)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if got != claims {
		t.Fatalf("claims = %+v, want %+v", got, claims)
	}
}

func TestCallerTokenGenerateClaims(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	claims, err := GenerateCallerTokenClaims(" user:abc ", now)
	if err != nil {
		t.Fatalf("GenerateCallerTokenClaims: %v", err)
	}

	if claims.SubjectID != "user:abc" {
		t.Fatalf("SubjectID = %q, want %q", claims.SubjectID, "user:abc")
	}
	if claims.IssuedAt != now.Unix() {
		t.Fatalf("IssuedAt = %d, want %d", claims.IssuedAt, now.Unix())
	}
	if claims.ExpiresAt != now.Add(callerTokenTTL).Unix() {
		t.Fatalf("ExpiresAt = %d, want %d", claims.ExpiresAt, now.Add(callerTokenTTL).Unix())
	}
	if claims.Issuer != callerTokenIssuer {
		t.Fatalf("Issuer = %q, want %q", claims.Issuer, callerTokenIssuer)
	}
	if claims.Audience != callerTokenAudience {
		t.Fatalf("Audience = %q, want %q", claims.Audience, callerTokenAudience)
	}
	if claims.ID == "" {
		t.Fatal("ID = empty, want generated id")
	}
}

func TestCallerTokenGenerateClaimsRequiresSubjectID(t *testing.T) {
	t.Parallel()

	_, err := GenerateCallerTokenClaims(" ", time.Now())
	if err == nil {
		t.Fatal("GenerateCallerTokenClaims error = nil, want error")
	}
}

func TestCallerTokenVerifyRejectsTamperedClaims(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	claims, err := GenerateCallerTokenClaims("user:123", time.Now().UTC())
	if err != nil {
		t.Fatalf("GenerateCallerTokenClaims: %v", err)
	}
	token, err := Issue(claims, secret)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d, want 3", len(parts))
	}
	claims.SubjectID = "user:admin"
	tamperedClaims, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("Marshal tampered claims: %v", err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(tamperedClaims)
	tamperedToken := strings.Join(parts, ".")

	if _, err := Verify(tamperedToken, secret); err == nil {
		t.Fatal("Verify tampered token error = nil, want error")
	}
}

func TestCallerTokenVerifyRejectsWrongSecret(t *testing.T) {
	t.Parallel()

	claims, err := GenerateCallerTokenClaims("user:123", time.Now().UTC())
	if err != nil {
		t.Fatalf("GenerateCallerTokenClaims: %v", err)
	}
	token, err := Issue(claims, []byte("test-secret"))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := Verify(token, []byte("other-secret")); err == nil {
		t.Fatal("Verify with wrong secret error = nil, want error")
	}
}

func TestCallerTokenVerifyRejectsExpiredToken(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	claims := CallerTokenClaims{
		SubjectID: "user:123",
		IssuedAt:  now.Add(-10 * time.Minute).Unix(),
		ExpiresAt: now.Add(-5 * time.Minute).Unix(),
		Issuer:    callerTokenIssuer,
		Audience:  callerTokenAudience,
		ID:        "expired-token",
	}
	token, err := Issue(claims, []byte("test-secret"))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if _, err := Verify(token, []byte("test-secret")); err == nil {
		t.Fatal("Verify expired token error = nil, want error")
	}
}

func TestCallerTokenVerifyRejectsLifetimeAboveMaximum(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	claims := CallerTokenClaims{
		SubjectID: "user:123",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(callerTokenTTL + time.Second).Unix(),
		Issuer:    callerTokenIssuer,
		Audience:  callerTokenAudience,
		ID:        "long-lived-token",
	}
	token, err := issueCallerTokenWithoutValidation(t, claims, []byte("test-secret"))
	if err != nil {
		t.Fatalf("issueCallerTokenWithoutValidation: %v", err)
	}

	if _, err := Verify(token, []byte("test-secret")); err == nil {
		t.Fatal("Verify long-lived token error = nil, want error")
	}
}

func TestCallerTokenVerifyRejectsInvalidIssuerAndAudience(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	base := CallerTokenClaims{
		SubjectID: "user:123",
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Minute).Unix(),
		Issuer:    callerTokenIssuer,
		Audience:  callerTokenAudience,
		ID:        "token-id",
	}

	for _, tc := range []struct {
		name   string
		claims CallerTokenClaims
	}{
		{
			name: "issuer",
			claims: CallerTokenClaims{
				SubjectID: base.SubjectID,
				IssuedAt:  base.IssuedAt,
				ExpiresAt: base.ExpiresAt,
				Issuer:    "other-issuer",
				Audience:  base.Audience,
				ID:        base.ID,
			},
		},
		{
			name: "audience",
			claims: CallerTokenClaims{
				SubjectID: base.SubjectID,
				IssuedAt:  base.IssuedAt,
				ExpiresAt: base.ExpiresAt,
				Issuer:    base.Issuer,
				Audience:  "other-audience",
				ID:        base.ID,
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			token, err := issueCallerTokenWithoutValidation(t, tc.claims, []byte("test-secret"))
			if err != nil {
				t.Fatalf("issueCallerTokenWithoutValidation: %v", err)
			}
			if _, err := Verify(token, []byte("test-secret")); err == nil {
				t.Fatal("Verify error = nil, want error")
			}
		})
	}
}

func issueCallerTokenWithoutValidation(t testing.TB, claims CallerTokenClaims, secret []byte) (string, error) {
	t.Helper()
	header := callerTokenHeader{Algorithm: callerTokenAlgorithm, Type: callerTokenType}
	encodedHeader, err := encodeCallerTokenPart(header)
	if err != nil {
		return "", err
	}
	encodedClaims, err := encodeCallerTokenPart(claims)
	if err != nil {
		return "", err
	}
	signingInput := encodedHeader + "." + encodedClaims
	return signingInput + "." + signCallerToken(signingInput, secret), nil
}
