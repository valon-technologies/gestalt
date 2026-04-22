package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
)

const integrationOAuthStateTTL = 10 * time.Minute
const pendingConnectionTTL = 30 * time.Minute

var errPendingConnectionExpired = errors.New("pending connection expired")

type integrationOAuthState struct {
	SubjectID        string            `json:"sid"`
	AuthSource       string            `json:"src,omitempty"`
	Integration      string            `json:"int"`
	Connection       string            `json:"con,omitempty"`
	Instance         string            `json:"ins,omitempty"`
	Verifier         string            `json:"ver,omitempty"`
	ConnectionParams map[string]string `json:"cp,omitempty"`
	ExpiresAt        int64             `json:"exp"`
}

type integrationOAuthStateCodec struct {
	encryptor *cryptoutil.AESGCMEncryptor
}

func newIntegrationOAuthStateCodec(secret []byte) (*integrationOAuthStateCodec, error) {
	encryptor, err := cryptoutil.NewAESGCM(secret)
	if err != nil {
		return nil, fmt.Errorf("create oauth state encryptor: %w", err)
	}
	return &integrationOAuthStateCodec{encryptor: encryptor}, nil
}

func encodeEncryptedState[T any](enc *cryptoutil.AESGCMEncryptor, label string, state T) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal %s: %w", label, err)
	}
	encoded, err := enc.EncryptURLSafe(string(payload))
	if err != nil {
		return "", fmt.Errorf("encrypt %s: %w", label, err)
	}
	return encoded, nil
}

func decodeEncryptedState[T any](enc *cryptoutil.AESGCMEncryptor, label, encoded string) (*T, error) {
	plaintext, err := enc.DecryptURLSafe(encoded)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", label, err)
	}

	var state T
	if err := json.Unmarshal([]byte(plaintext), &state); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", label, err)
	}
	return &state, nil
}

func validateIntegrationOAuthState(state *integrationOAuthState, now time.Time) error {
	if state.SubjectID == "" {
		return fmt.Errorf("oauth state missing subject ID")
	}
	if state.Integration == "" {
		return fmt.Errorf("oauth state missing integration")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("oauth state missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return fmt.Errorf("oauth state expired")
	}
	return nil
}

func (c *integrationOAuthStateCodec) Encode(state integrationOAuthState) (string, error) {
	return encodeEncryptedState(c.encryptor, "oauth state", state)
}

func (c *integrationOAuthStateCodec) Decode(encoded string, now time.Time) (*integrationOAuthState, error) {
	state, err := decodeEncryptedState[integrationOAuthState](c.encryptor, "oauth state", encoded)
	if err != nil {
		return nil, err
	}
	if err := validateIntegrationOAuthState(state, now); err != nil {
		return nil, err
	}
	return state, nil
}

const loginStateTTL = 10 * time.Minute
const loginStateCookieName = "login_state"
const mcpOAuthClientRegistrationTTL = 365 * 24 * time.Hour
const mcpOAuthAuthorizationCodeTTL = 5 * time.Minute
const mcpOAuthRefreshTokenTTL = 30 * 24 * time.Hour

const (
	mcpOAuthClientIDPrefix          = "gst_mcp_client_"
	mcpOAuthAuthorizationCodePrefix = "gst_mcp_code_"
	mcpOAuthRefreshTokenPrefix      = "gst_mcp_refresh_"
)

type loginState struct {
	State     string `json:"s"`
	Provider  string `json:"p,omitempty"`
	NextPath  string `json:"n,omitempty"`
	ExpiresAt int64  `json:"exp"`
}

type pendingConnectionState struct {
	Token      tokenMaterial             `json:"tok"`
	BindingKey string                    `json:"bind"`
	Candidates []core.DiscoveryCandidate `json:"cand"`
	ExpiresAt  int64                     `json:"exp"`
}

type pendingConnectionBindingState struct {
	BindingKey string `json:"bind"`
	ExpiresAt  int64  `json:"exp"`
}

type mcpOAuthClientRegistrationState struct {
	RedirectURIs            []string `json:"ru"`
	ClientName              string   `json:"cn,omitempty"`
	TokenEndpointAuthMethod string   `json:"tm,omitempty"`
	ExpiresAt               int64    `json:"exp"`
}

type mcpOAuthAuthorizationCodeState struct {
	ClientID            string `json:"cid"`
	RedirectURI         string `json:"ru"`
	Email               string `json:"em"`
	DisplayName         string `json:"dn,omitempty"`
	AvatarURL           string `json:"av,omitempty"`
	Scope               string `json:"sc,omitempty"`
	CodeChallenge       string `json:"cc"`
	CodeChallengeMethod string `json:"cm,omitempty"`
	ExpiresAt           int64  `json:"exp"`
}

type mcpOAuthRefreshTokenState struct {
	ClientID    string `json:"cid"`
	Email       string `json:"em"`
	DisplayName string `json:"dn,omitempty"`
	AvatarURL   string `json:"av,omitempty"`
	Scope       string `json:"sc,omitempty"`
	ExpiresAt   int64  `json:"exp"`
}

func encodeLoginState(enc *cryptoutil.AESGCMEncryptor, state loginState) (string, error) {
	return encodeEncryptedState(enc, "login state", state)
}

func validateLoginState(state *loginState, now time.Time) error {
	if state.State == "" {
		return fmt.Errorf("login state missing state value")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("login state missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return fmt.Errorf("login state expired")
	}
	return nil
}

func decodeLoginState(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*loginState, error) {
	state, err := decodeEncryptedState[loginState](enc, "login state", encoded)
	if err != nil {
		return nil, err
	}
	if err := validateLoginState(state, now); err != nil {
		return nil, err
	}
	return state, nil
}

func encodePendingConnectionState(enc *cryptoutil.AESGCMEncryptor, state pendingConnectionState) (string, error) {
	return encodeEncryptedState(enc, "pending connection state", state)
}

func validatePendingConnectionState(state *pendingConnectionState, now time.Time) error {
	if state.Token.SubjectID == "" {
		return fmt.Errorf("pending connection missing subject ID")
	}
	if state.Token.Integration == "" {
		return fmt.Errorf("pending connection missing integration")
	}
	if state.BindingKey == "" {
		return fmt.Errorf("pending connection missing binding key")
	}
	if len(state.Candidates) == 0 {
		return fmt.Errorf("pending connection missing candidates")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("pending connection missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return errPendingConnectionExpired
	}
	return nil
}

func decodePendingConnectionState(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*pendingConnectionState, error) {
	state, err := decodeEncryptedState[pendingConnectionState](enc, "pending connection state", encoded)
	if err != nil {
		return nil, err
	}
	if err := validatePendingConnectionState(state, now); err != nil {
		return nil, err
	}
	return state, nil
}

func encodePendingConnectionBindingState(enc *cryptoutil.AESGCMEncryptor, state pendingConnectionBindingState) (string, error) {
	return encodeEncryptedState(enc, "pending connection binding state", state)
}

func validatePendingConnectionBindingState(state *pendingConnectionBindingState, now time.Time) error {
	if state.BindingKey == "" {
		return fmt.Errorf("pending connection binding missing key")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("pending connection binding missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return errPendingConnectionExpired
	}
	return nil
}

func decodePendingConnectionBindingState(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*pendingConnectionBindingState, error) {
	state, err := decodeEncryptedState[pendingConnectionBindingState](enc, "pending connection binding state", encoded)
	if err != nil {
		return nil, err
	}
	if err := validatePendingConnectionBindingState(state, now); err != nil {
		return nil, err
	}
	return state, nil
}

func encodeMCPOAuthClientRegistration(enc *cryptoutil.AESGCMEncryptor, state mcpOAuthClientRegistrationState) (string, error) {
	encoded, err := encodeEncryptedState(enc, "mcp oauth client registration", state)
	if err != nil {
		return "", err
	}
	return mcpOAuthClientIDPrefix + encoded, nil
}

func validateMCPOAuthClientRegistration(state *mcpOAuthClientRegistrationState, now time.Time) error {
	if len(state.RedirectURIs) == 0 {
		return fmt.Errorf("mcp oauth client registration missing redirect URIs")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("mcp oauth client registration missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return fmt.Errorf("mcp oauth client registration expired")
	}
	return nil
}

func decodeMCPOAuthClientRegistration(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*mcpOAuthClientRegistrationState, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, mcpOAuthClientIDPrefix) {
		return nil, fmt.Errorf("mcp oauth client registration token is malformed")
	}
	state, err := decodeEncryptedState[mcpOAuthClientRegistrationState](enc, "mcp oauth client registration", strings.TrimPrefix(encoded, mcpOAuthClientIDPrefix))
	if err != nil {
		return nil, err
	}
	if err := validateMCPOAuthClientRegistration(state, now); err != nil {
		return nil, err
	}
	return state, nil
}

func encodeMCPOAuthAuthorizationCode(enc *cryptoutil.AESGCMEncryptor, state mcpOAuthAuthorizationCodeState) (string, error) {
	encoded, err := encodeEncryptedState(enc, "mcp oauth authorization code", state)
	if err != nil {
		return "", err
	}
	return mcpOAuthAuthorizationCodePrefix + encoded, nil
}

func validateMCPOAuthAuthorizationCode(state *mcpOAuthAuthorizationCodeState, now time.Time) error {
	if state.ClientID == "" {
		return fmt.Errorf("mcp oauth authorization code missing client ID")
	}
	if state.RedirectURI == "" {
		return fmt.Errorf("mcp oauth authorization code missing redirect URI")
	}
	if state.Email == "" {
		return fmt.Errorf("mcp oauth authorization code missing email")
	}
	if state.CodeChallenge == "" {
		return fmt.Errorf("mcp oauth authorization code missing code challenge")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("mcp oauth authorization code missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return fmt.Errorf("mcp oauth authorization code expired")
	}
	return nil
}

func decodeMCPOAuthAuthorizationCode(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*mcpOAuthAuthorizationCodeState, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, mcpOAuthAuthorizationCodePrefix) {
		return nil, fmt.Errorf("mcp oauth authorization code is malformed")
	}
	state, err := decodeEncryptedState[mcpOAuthAuthorizationCodeState](enc, "mcp oauth authorization code", strings.TrimPrefix(encoded, mcpOAuthAuthorizationCodePrefix))
	if err != nil {
		return nil, err
	}
	if err := validateMCPOAuthAuthorizationCode(state, now); err != nil {
		return nil, err
	}
	return state, nil
}

func encodeMCPOAuthRefreshToken(enc *cryptoutil.AESGCMEncryptor, state mcpOAuthRefreshTokenState) (string, error) {
	encoded, err := encodeEncryptedState(enc, "mcp oauth refresh token", state)
	if err != nil {
		return "", err
	}
	return mcpOAuthRefreshTokenPrefix + encoded, nil
}

func validateMCPOAuthRefreshToken(state *mcpOAuthRefreshTokenState, now time.Time) error {
	if state.ClientID == "" {
		return fmt.Errorf("mcp oauth refresh token missing client ID")
	}
	if state.Email == "" {
		return fmt.Errorf("mcp oauth refresh token missing email")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("mcp oauth refresh token missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return fmt.Errorf("mcp oauth refresh token expired")
	}
	return nil
}

func decodeMCPOAuthRefreshToken(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*mcpOAuthRefreshTokenState, error) {
	encoded = strings.TrimSpace(encoded)
	if !strings.HasPrefix(encoded, mcpOAuthRefreshTokenPrefix) {
		return nil, fmt.Errorf("mcp oauth refresh token is malformed")
	}
	state, err := decodeEncryptedState[mcpOAuthRefreshTokenState](enc, "mcp oauth refresh token", strings.TrimPrefix(encoded, mcpOAuthRefreshTokenPrefix))
	if err != nil {
		return nil, err
	}
	if err := validateMCPOAuthRefreshToken(state, now); err != nil {
		return nil, err
	}
	return state, nil
}

func setURLQueryParam(rawURL, key, value string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
