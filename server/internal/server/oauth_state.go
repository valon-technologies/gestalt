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

func (c *integrationOAuthStateCodec) Encode(state integrationOAuthState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal oauth state: %w", err)
	}
	encoded, err := c.encryptor.EncryptURLSafe(string(payload))
	if err != nil {
		return "", fmt.Errorf("encrypt oauth state: %w", err)
	}
	return encoded, nil
}

func (c *integrationOAuthStateCodec) Decode(encoded string, now time.Time) (*integrationOAuthState, error) {
	plaintext, err := c.encryptor.DecryptURLSafe(encoded)
	if err != nil {
		return nil, fmt.Errorf("decrypt oauth state: %w", err)
	}

	var state integrationOAuthState
	if err := json.Unmarshal([]byte(plaintext), &state); err != nil {
		return nil, fmt.Errorf("unmarshal oauth state: %w", err)
	}
	if state.UserID == "" {
		return nil, fmt.Errorf("oauth state missing user ID")
	}
	if state.Integration == "" {
		return nil, fmt.Errorf("oauth state missing integration")
	}
	if state.ExpiresAt == 0 {
		return nil, fmt.Errorf("oauth state missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return nil, fmt.Errorf("oauth state expired")
	}
	return &state, nil
}

const loginStateTTL = 10 * time.Minute
const loginStateCookieName = "login_state"

type loginState struct {
	State     string `json:"s"`
	ExpiresAt int64  `json:"exp"`
}

type pendingConnectionState struct {
	UserID         string                    `json:"uid"`
	Integration    string                    `json:"int"`
	Connection     string                    `json:"con,omitempty"`
	Instance       string                    `json:"ins,omitempty"`
	AccessToken    string                    `json:"at"`
	RefreshToken   string                    `json:"rt,omitempty"`
	TokenExpiresAt int64                     `json:"tex,omitempty"`
	MetadataJSON   string                    `json:"meta,omitempty"`
	Candidates     []core.DiscoveryCandidate `json:"cand"`
	ExpiresAt      int64                     `json:"exp"`
}

func encodeLoginState(enc *cryptoutil.AESGCMEncryptor, state loginState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal login state: %w", err)
	}
	return enc.EncryptURLSafe(string(payload))
}

func decodeLoginState(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*loginState, error) {
	plaintext, err := enc.DecryptURLSafe(encoded)
	if err != nil {
		return nil, fmt.Errorf("decrypt login state: %w", err)
	}
	var state loginState
	if err := json.Unmarshal([]byte(plaintext), &state); err != nil {
		return nil, fmt.Errorf("unmarshal login state: %w", err)
	}
	if state.State == "" {
		return nil, fmt.Errorf("login state missing state value")
	}
	if now.Unix() > state.ExpiresAt {
		return nil, fmt.Errorf("login state expired")
	}
	return &state, nil
}

func encodePendingConnectionState(enc *cryptoutil.AESGCMEncryptor, state pendingConnectionState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("marshal pending connection state: %w", err)
	}
	return enc.EncryptURLSafe(string(payload))
}

func decodePendingConnectionState(enc *cryptoutil.AESGCMEncryptor, encoded string, now time.Time) (*pendingConnectionState, error) {
	plaintext, err := enc.DecryptURLSafe(encoded)
	if err != nil {
		return nil, fmt.Errorf("decrypt pending connection state: %w", err)
	}
	var state pendingConnectionState
	if err := json.Unmarshal([]byte(plaintext), &state); err != nil {
		return nil, fmt.Errorf("unmarshal pending connection state: %w", err)
	}
	if state.UserID == "" {
		return nil, fmt.Errorf("pending connection missing user ID")
	}
	if state.Integration == "" {
		return nil, fmt.Errorf("pending connection missing integration")
	}
	if len(state.Candidates) == 0 {
		return nil, fmt.Errorf("pending connection missing candidates")
	}
	if state.ExpiresAt == 0 {
		return nil, fmt.Errorf("pending connection missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return nil, errPendingConnectionExpired
	}
	return &state, nil
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
