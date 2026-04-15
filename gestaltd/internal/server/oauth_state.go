package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
)

const integrationOAuthStateTTL = 10 * time.Minute
const pendingConnectionTTL = 30 * time.Minute

var errPendingConnectionExpired = errors.New("pending connection expired")

type integrationOAuthState struct {
	UserID           string                  `json:"uid,omitempty"`
	OwnerKind        string                  `json:"ok,omitempty"`
	OwnerID          string                  `json:"oid,omitempty"`
	InitiatorUserID  string                  `json:"iuid,omitempty"`
	AuthSource       string                  `json:"src,omitempty"`
	ViewerScopes     []string                `json:"vsc,omitempty"`
	ViewerPerms      []core.AccessPermission `json:"vpm,omitempty"`
	Integration      string                  `json:"int"`
	Connection       string                  `json:"con,omitempty"`
	Instance         string                  `json:"ins,omitempty"`
	ReturnPath       string                  `json:"ret,omitempty"`
	CallbackPort     int                     `json:"cbp,omitempty"`
	CallbackState    string                  `json:"cbs,omitempty"`
	Verifier         string                  `json:"ver,omitempty"`
	ConnectionParams map[string]string       `json:"cp,omitempty"`
	ExpiresAt        int64                   `json:"exp"`
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
	normalizeIntegrationOAuthState(state)
	if state.OwnerKind == "" || state.OwnerID == "" {
		return fmt.Errorf("oauth state missing owner")
	}
	if state.InitiatorUserID == "" {
		return fmt.Errorf("oauth state missing initiator user ID")
	}
	if state.Integration == "" {
		return fmt.Errorf("oauth state missing integration")
	}
	if _, err := normalizeReturnPath(state.ReturnPath); err != nil {
		return err
	}
	if err := validateManagedIdentityReturnPath(state.ReturnPath, state.OwnerKind, state.OwnerID); err != nil {
		return err
	}
	switch {
	case state.CallbackPort == 0 && state.CallbackState == "":
	case state.CallbackPort <= 0 || state.CallbackPort > maxPort || strings.TrimSpace(state.CallbackState) == "":
		return fmt.Errorf("oauth state missing callback binding")
	}
	if state.ExpiresAt == 0 {
		return fmt.Errorf("oauth state missing expiration")
	}
	if now.Unix() > state.ExpiresAt {
		return fmt.Errorf("oauth state expired")
	}
	return nil
}

func normalizeIntegrationOAuthState(state *integrationOAuthState) {
	if state == nil {
		return
	}
	if state.OwnerKind == "" {
		if strings.TrimSpace(state.OwnerID) != "" {
			state.OwnerKind = core.IntegrationTokenOwnerKindUser
		} else if strings.TrimSpace(state.UserID) != "" {
			state.OwnerKind = core.IntegrationTokenOwnerKindUser
			state.OwnerID = strings.TrimSpace(state.UserID)
		}
	}
	if state.OwnerID == "" && state.OwnerKind == core.IntegrationTokenOwnerKindUser {
		state.OwnerID = strings.TrimSpace(state.UserID)
	}
	if state.UserID == "" && state.OwnerKind == core.IntegrationTokenOwnerKindUser {
		state.UserID = strings.TrimSpace(state.OwnerID)
	}
	if state.InitiatorUserID == "" {
		switch state.OwnerKind {
		case core.IntegrationTokenOwnerKindUser:
			state.InitiatorUserID = strings.TrimSpace(state.OwnerID)
		default:
			state.InitiatorUserID = strings.TrimSpace(state.UserID)
		}
	}
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

func integrationOAuthRedirectPath(state *integrationOAuthState) string {
	if state == nil {
		return "/integrations"
	}
	if state.ReturnPath != "" {
		return state.ReturnPath
	}
	if state.OwnerKind == core.IntegrationTokenOwnerKindManagedIdentity && strings.TrimSpace(state.OwnerID) != "" {
		u := &url.URL{Path: "/identities"}
		q := u.Query()
		q.Set("id", state.OwnerID)
		u.RawQuery = q.Encode()
		return u.String()
	}
	return "/integrations"
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
	ReturnPath string                    `json:"ret,omitempty"`
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
	if _, err := normalizeReturnPath(state.ReturnPath); err != nil {
		return err
	}
	if err := validateManagedIdentityReturnPath(state.ReturnPath, state.Token.OwnerKind, state.Token.OwnerID); err != nil {
		return err
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

func normalizeReturnPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if containsASCIIControl(raw) || containsInvalidPercentEscape(raw) || containsEncodedASCIIControl(raw) {
		return "", fmt.Errorf("invalid returnPath")
	}
	if strings.Contains(raw, `\`) || strings.Contains(strings.ToLower(raw), "%5c") {
		return "", fmt.Errorf("invalid returnPath")
	}
	if strings.HasPrefix(raw, "//") {
		return "", fmt.Errorf("invalid returnPath")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid returnPath")
	}
	if u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "", fmt.Errorf("invalid returnPath")
	}
	u.Path = path.Clean(u.Path)
	u.RawPath = ""
	return u.String(), nil
}

func validateManagedIdentityReturnPath(raw, ownerKind, ownerID string) error {
	if raw == "" || ownerKind != core.IntegrationTokenOwnerKindManagedIdentity {
		return nil
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid returnPath")
	}
	targetID, err := managedIdentityTargetIDFromReturnPath(u)
	if err != nil {
		return err
	}
	if targetID != "" && targetID != ownerID {
		return fmt.Errorf("invalid returnPath")
	}
	return nil
}

func managedIdentityTargetIDFromReturnPath(u *url.URL) (string, error) {
	if u == nil {
		return "", nil
	}
	trimmedPath := strings.Trim(strings.TrimSpace(u.Path), "/")
	if trimmedPath == "identities" {
		return strings.TrimSpace(u.Query().Get("id")), nil
	}
	segments := strings.Split(trimmedPath, "/")
	if len(segments) >= 2 && segments[0] == "identities" {
		id, err := url.PathUnescape(segments[1])
		if err != nil {
			return "", fmt.Errorf("invalid returnPath")
		}
		return strings.TrimSpace(id), nil
	}
	return "", nil
}

func containsASCIIControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func containsInvalidPercentEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+2 >= len(s) || !isHexDigit(s[i+1]) || !isHexDigit(s[i+2]) {
			return true
		}
		i += 2
	}
	return false
}

func containsEncodedASCIIControl(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			continue
		}
		if v < 0x20 || v == 0x7f {
			return true
		}
	}
	return false
}

func isHexDigit(b byte) bool {
	return ('0' <= b && b <= '9') || ('a' <= b && b <= 'f') || ('A' <= b && b <= 'F')
}
