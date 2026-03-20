package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	AuthModePublic            = "public"
	AuthModeSigned            = "signed"
	AuthModeTrustedUserHeader = "trusted_user_header"

	DefaultSignatureHeader = "X-Webhook-Signature"
)

func verifySignature(secret, body []byte, signatureHex string) error {
	sig, err := hex.DecodeString(signatureHex)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
