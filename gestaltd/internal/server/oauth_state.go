package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
)

const integrationOAuthStateTTL = 10 * time.Minute
const pendingConnectionTTL = 30 * time.Minute

var errPendingConnectionExpired = errors.New("pending connection expired")

type integrationOAuthState struct {
	UserID           string            `json:"uid"`
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
	if state.UserID == "" {
		return fmt.Errorf("oauth state missing user ID")
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

type loginState struct {
	State     string `json:"s"`
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
