package providergateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	callerTokenAlgorithm = "HS256"
	callerTokenType      = "JWT"
	callerTokenIssuer    = "gestaltd"
	callerTokenAudience  = "provider-gateway"
	callerTokenTTL       = 5 * time.Minute
)

type CallerTokenClaims struct {
	SubjectID string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	ID        string `json:"jti"`
}

type callerTokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func GenerateCallerTokenClaims(subjectID string, now time.Time) (CallerTokenClaims, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return CallerTokenClaims{}, fmt.Errorf("caller token: subject id is required")
	}
	now = now.UTC()
	return CallerTokenClaims{
		SubjectID: subjectID,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(callerTokenTTL).Unix(),
		Issuer:    callerTokenIssuer,
		Audience:  callerTokenAudience,
		ID:        uuid.NewString(),
	}, nil
}

func Issue(claims CallerTokenClaims, secret []byte) (string, error) {
	if err := validateCallerTokenSecret(secret); err != nil {
		return "", err
	}
	if err := validateCallerTokenClaims(claims); err != nil {
		return "", err
	}
	header := callerTokenHeader{
		Algorithm: callerTokenAlgorithm,
		Type:      callerTokenType,
	}
	encodedHeader, err := encodeCallerTokenPart(header)
	if err != nil {
		return "", fmt.Errorf("caller token: encode header: %w", err)
	}
	encodedClaims, err := encodeCallerTokenPart(claims)
	if err != nil {
		return "", fmt.Errorf("caller token: encode claims: %w", err)
	}
	signingInput := encodedHeader + "." + encodedClaims
	signature := signCallerToken(signingInput, secret)
	return signingInput + "." + signature, nil
}

func Verify(token string, secret []byte) (CallerTokenClaims, error) {
	if err := validateCallerTokenSecret(secret); err != nil {
		return CallerTokenClaims{}, err
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return CallerTokenClaims{}, fmt.Errorf("caller token: invalid token format")
	}
	var header callerTokenHeader
	if err := decodeCallerTokenPart(parts[0], &header); err != nil {
		return CallerTokenClaims{}, fmt.Errorf("caller token: decode header: %w", err)
	}
	if header.Algorithm != callerTokenAlgorithm || header.Type != callerTokenType {
		return CallerTokenClaims{}, fmt.Errorf("caller token: unsupported token header")
	}
	signingInput := parts[0] + "." + parts[1]
	expectedSignature := signCallerToken(signingInput, secret)
	if !hmac.Equal([]byte(expectedSignature), []byte(parts[2])) {
		return CallerTokenClaims{}, fmt.Errorf("caller token: invalid signature")
	}
	var claims CallerTokenClaims
	if err := decodeCallerTokenPart(parts[1], &claims); err != nil {
		return CallerTokenClaims{}, fmt.Errorf("caller token: decode claims: %w", err)
	}
	if err := validateCallerTokenClaims(claims); err != nil {
		return CallerTokenClaims{}, err
	}
	if time.Now().UTC().Unix() >= claims.ExpiresAt {
		return CallerTokenClaims{}, fmt.Errorf("caller token: expired")
	}
	return claims, nil
}

func validateCallerTokenSecret(secret []byte) error {
	if len(secret) == 0 {
		return fmt.Errorf("caller token: secret is required")
	}
	return nil
}

func validateCallerTokenClaims(claims CallerTokenClaims) error {
	if strings.TrimSpace(claims.SubjectID) == "" {
		return fmt.Errorf("caller token: subject id is required")
	}
	if claims.Issuer != callerTokenIssuer {
		return fmt.Errorf("caller token: invalid issuer")
	}
	if claims.Audience != callerTokenAudience {
		return fmt.Errorf("caller token: invalid audience")
	}
	if claims.ID == "" {
		return fmt.Errorf("caller token: id is required")
	}
	if claims.IssuedAt <= 0 {
		return fmt.Errorf("caller token: issued at is required")
	}
	if claims.ExpiresAt <= claims.IssuedAt {
		return fmt.Errorf("caller token: expires at must be after issued at")
	}
	if claims.ExpiresAt-claims.IssuedAt > int64(callerTokenTTL/time.Second) {
		return fmt.Errorf("caller token: lifetime exceeds maximum")
	}
	return nil
}

func encodeCallerTokenPart(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeCallerTokenPart(encoded string, out any) error {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func signCallerToken(signingInput string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
