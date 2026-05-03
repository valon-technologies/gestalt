package agentgrant

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
	sealVersion          = "v1"
	sealPurposeToolID    = "agent-tool-id"
	sealPurposeToolScope = "agent-tool-scope"
	toolIDPrefix         = "agt_tool_"
)

type toolBinding struct {
	Target coreagent.ToolTarget `json:"target"`
}

func (m *Manager) MintToolID(target coreagent.ToolTarget) (string, error) {
	if m == nil {
		return "", fmt.Errorf("agent run grants are not available")
	}
	target = coreagent.ToolTarget{
		System:         strings.TrimSpace(target.System),
		Plugin:         strings.TrimSpace(target.Plugin),
		Operation:      strings.TrimSpace(target.Operation),
		Connection:     strings.TrimSpace(target.Connection),
		Instance:       strings.TrimSpace(target.Instance),
		CredentialMode: core.ConnectionMode(strings.TrimSpace(string(target.CredentialMode))),
	}
	if target.Operation == "" || (target.Plugin == "" && target.System == "") || (target.Plugin != "" && target.System != "") {
		return "", fmt.Errorf("agent tool target is incomplete")
	}
	sealed, err := m.sealValue(sealPurposeToolID, toolBinding{Target: target})
	if err != nil {
		return "", err
	}
	return toolIDPrefix + sealed, nil
}

func (m *Manager) ResolveToolID(id string) (coreagent.ToolTarget, error) {
	if m == nil {
		return coreagent.ToolTarget{}, fmt.Errorf("agent run grants are not available")
	}
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, toolIDPrefix) {
		return coreagent.ToolTarget{}, fmt.Errorf("agent tool id is invalid")
	}
	var binding toolBinding
	if err := m.openValue(sealPurposeToolID, strings.TrimPrefix(id, toolIDPrefix), &binding); err != nil {
		return coreagent.ToolTarget{}, fmt.Errorf("agent tool id is invalid")
	}
	target := binding.Target
	target.System = strings.TrimSpace(target.System)
	target.Plugin = strings.TrimSpace(target.Plugin)
	target.Operation = strings.TrimSpace(target.Operation)
	target.Connection = strings.TrimSpace(target.Connection)
	target.Instance = strings.TrimSpace(target.Instance)
	target.CredentialMode = core.ConnectionMode(strings.TrimSpace(string(target.CredentialMode)))
	if target.Operation == "" || (target.Plugin == "" && target.System == "") || (target.Plugin != "" && target.System != "") {
		return coreagent.ToolTarget{}, fmt.Errorf("agent tool id is invalid")
	}
	return target, nil
}

func (m *Manager) sealValue(purpose string, value any) (string, error) {
	plaintext, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode agent grant payload: %w", err)
	}
	gcm, err := m.sealer(purpose)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate agent grant nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, []byte(purpose))
	return sealVersion + "_" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (m *Manager) openValue(purpose, token string, value any) error {
	token = strings.TrimSpace(token)
	prefix := sealVersion + "_"
	if !strings.HasPrefix(token, prefix) {
		return fmt.Errorf("agent grant payload version is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, prefix))
	if err != nil {
		return fmt.Errorf("decode agent grant payload: %w", err)
	}
	gcm, err := m.sealer(purpose)
	if err != nil {
		return err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) <= nonceSize {
		return fmt.Errorf("agent grant payload is invalid")
	}
	plaintext, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], []byte(purpose))
	if err != nil {
		return fmt.Errorf("open agent grant payload: %w", err)
	}
	if err := json.Unmarshal(plaintext, value); err != nil {
		return fmt.Errorf("decode agent grant payload: %w", err)
	}
	return nil
}

func (m *Manager) sealer(purpose string) (cipher.AEAD, error) {
	if m == nil {
		return nil, fmt.Errorf("agent run grants are not available")
	}
	key := deriveSealKey(m.secret, purpose)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("initialize agent grant sealer: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize agent grant sealer: %w", err)
	}
	return gcm, nil
}

func deriveSealKey(secret []byte, purpose string) [32]byte {
	h := sha256.New()
	_, _ = h.Write([]byte("gestalt-agentgrant"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(purpose))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(secret)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
