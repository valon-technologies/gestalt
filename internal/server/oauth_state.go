package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	cryptoutil "github.com/valon-technologies/toolshed/core/crypto"
)

const integrationOAuthStateTTL = 10 * time.Minute

type integrationOAuthState struct {
	UserID      string `json:"uid"`
	Integration string `json:"int"`
	Verifier    string `json:"ver,omitempty"`
	ExpiresAt   int64  `json:"exp"`
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
	encoded, err := c.encryptor.Encrypt(string(payload))
	if err != nil {
		return "", fmt.Errorf("encrypt oauth state: %w", err)
	}
	return encoded, nil
}

func (c *integrationOAuthStateCodec) Decode(encoded string, now time.Time) (*integrationOAuthState, error) {
	plaintext, err := c.encryptor.Decrypt(encoded)
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
