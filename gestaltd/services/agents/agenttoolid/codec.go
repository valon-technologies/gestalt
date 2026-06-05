package agenttoolid

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

const (
	sealVersion       = "v1"
	sealPurposeToolID = "agent-tool-id-v2"
	toolIDPrefix      = "agt_tool_"
)

type Codec struct {
	secret []byte
}

type toolBinding struct {
	Target coreagent.ToolTarget `json:"target"`
}

func NewCodec(secret []byte) (*Codec, error) {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("generate agent tool id secret: %w", err)
		}
	}
	return &Codec{secret: append([]byte(nil), secret...)}, nil
}

func (c *Codec) Mint(target coreagent.ToolTarget) (string, error) {
	if c == nil {
		return "", fmt.Errorf("agent tool id codec is not available")
	}
	target = normalizeToolTarget(target)
	if err := validateToolTarget(target); err != nil {
		return "", err
	}
	sealed, err := c.sealValue(sealPurposeToolID, toolBinding{Target: target})
	if err != nil {
		return "", err
	}
	return toolIDPrefix + sealed, nil
}

func (c *Codec) Resolve(id string) (coreagent.ToolTarget, error) {
	if c == nil {
		return coreagent.ToolTarget{}, fmt.Errorf("agent tool id codec is not available")
	}
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, toolIDPrefix) {
		return coreagent.ToolTarget{}, fmt.Errorf("agent tool id is invalid")
	}
	var binding toolBinding
	if err := c.openValue(sealPurposeToolID, strings.TrimPrefix(id, toolIDPrefix), &binding); err != nil {
		return coreagent.ToolTarget{}, fmt.Errorf("agent tool id is invalid")
	}
	target := normalizeToolTarget(binding.Target)
	if err := validateToolTarget(target); err != nil {
		return coreagent.ToolTarget{}, fmt.Errorf("agent tool id is invalid")
	}
	return target, nil
}

func normalizeToolTarget(target coreagent.ToolTarget) coreagent.ToolTarget {
	return coreagent.ToolTarget{
		System:         strings.TrimSpace(target.System),
		App:            strings.TrimSpace(target.App),
		Operation:      strings.TrimSpace(target.Operation),
		Connection:     strings.TrimSpace(target.Connection),
		Instance:       strings.TrimSpace(target.Instance),
		CredentialMode: core.NormalizeOptionalConnectionMode(target.CredentialMode),
		Unavailable:    normalizeUnavailableToolTarget(target.Unavailable),
		RunAs:          core.NormalizeRunAsSubject(target.RunAs),
	}
}

func validateToolTarget(target coreagent.ToolTarget) error {
	if target.Unavailable != nil {
		if target.App == "" || target.System != "" || target.Operation != "" {
			return fmt.Errorf("agent tool target is incomplete")
		}
		return nil
	}
	if target.Operation == "" || (target.App == "" && target.System == "") || (target.App != "" && target.System != "") {
		return fmt.Errorf("agent tool target is incomplete")
	}
	return nil
}

func normalizeUnavailableToolTarget(value *coreagent.UnavailableToolTarget) *coreagent.UnavailableToolTarget {
	if value == nil {
		return nil
	}
	reason := strings.TrimSpace(value.Reason)
	if reason == "" {
		return nil
	}
	return &coreagent.UnavailableToolTarget{
		Reason:  reason,
		Message: strings.TrimSpace(value.Message),
	}
}

func (c *Codec) sealValue(purpose string, value any) (string, error) {
	plaintext, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode agent tool id payload: %w", err)
	}
	gcm, err := c.sealer(purpose)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate agent tool id nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, []byte(purpose))
	return sealVersion + "_" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *Codec) openValue(purpose, token string, value any) error {
	token = strings.TrimSpace(token)
	prefix := sealVersion + "_"
	if !strings.HasPrefix(token, prefix) {
		return fmt.Errorf("agent tool id payload version is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, prefix))
	if err != nil {
		return fmt.Errorf("decode agent tool id payload: %w", err)
	}
	gcm, err := c.sealer(purpose)
	if err != nil {
		return err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) <= nonceSize {
		return fmt.Errorf("agent tool id payload is invalid")
	}
	plaintext, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], []byte(purpose))
	if err != nil {
		return fmt.Errorf("open agent tool id payload: %w", err)
	}
	if err := json.Unmarshal(plaintext, value); err != nil {
		return fmt.Errorf("decode agent tool id payload: %w", err)
	}
	return nil
}

func (c *Codec) sealer(purpose string) (cipher.AEAD, error) {
	if c == nil {
		return nil, fmt.Errorf("agent tool id codec is not available")
	}
	key := deriveSealKey(c.secret, purpose)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("initialize agent tool id sealer: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize agent tool id sealer: %w", err)
	}
	return gcm, nil
}

func deriveSealKey(secret []byte, purpose string) [32]byte {
	h := sha256.New()
	_, _ = h.Write([]byte("gestalt-agenttoolid"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(purpose))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(secret)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
