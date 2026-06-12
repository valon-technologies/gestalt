package providergateway

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	callerTokenAlgorithm = "EdDSA"
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

type CallerTokenIssuer struct {
	privateKey ed25519.PrivateKey
}

func NewCallerTokenIssuer(privateKeyPEM string) (*CallerTokenIssuer, error) {
	privateKeyPEM = strings.TrimSpace(privateKeyPEM)
	if privateKeyPEM == "" {
		return nil, nil
	}
	privateKey, err := parseCallerTokenPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	return &CallerTokenIssuer{privateKey: privateKey}, nil
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

func (i *CallerTokenIssuer) Issue(claims CallerTokenClaims) (string, error) {
	if i == nil || len(i.privateKey) == 0 {
		return "", fmt.Errorf("caller token: private key is required")
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
	signature := signCallerToken(signingInput, i.privateKey)
	return signingInput + "." + signature, nil
}

func Verify(token string, publicKeyPEM string) (CallerTokenClaims, error) {
	publicKey, err := parseCallerTokenPublicKey(publicKeyPEM)
	if err != nil {
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
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return CallerTokenClaims{}, fmt.Errorf("caller token: decode signature: %w", err)
	}
	if !ed25519.Verify(publicKey, []byte(signingInput), signature) {
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

func parseCallerTokenPrivateKey(privateKeyPEM string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(privateKeyPEM)))
	if block == nil {
		return nil, fmt.Errorf("caller token: decode private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("caller token: parse private key: %w", err)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("caller token: private key must be Ed25519")
	}
	return privateKey, nil
}

func parseCallerTokenPublicKey(publicKeyPEM string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(publicKeyPEM)))
	if block == nil {
		return nil, fmt.Errorf("caller token: decode public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("caller token: parse public key: %w", err)
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("caller token: public key must be Ed25519")
	}
	return publicKey, nil
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

func signCallerToken(signingInput string, privateKey ed25519.PrivateKey) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(signingInput)))
}
