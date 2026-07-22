package tunnel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// tunnelHostAlphabet excludes characters that could confuse an HTTP CONNECT
// host header parser; frp custom domains are DNS-like labels.
const tunnelHostAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// Identity is an in-memory ECDSA P-256 key and self-signed certificate whose
// SAN is a random 128-bit tunnel host. Never persisted.
type Identity struct {
	Certificate tls.Certificate
	SPKISHA256  string // base64url SHA-256 of the SubjectPublicKeyInfo
	TunnelHost  string // random 128-bit host label
}

// NewIdentity generates a fresh ECDSA P-256 key, a self-signed certificate with
// a random 128-bit tunnel host as its sole DNS SAN, and computes the SPKI SHA-256.
func NewIdentity() (*Identity, error) {
	host, err := randomTunnelHost()
	if err != nil {
		return nil, fmt.Errorf("tunnel identity: %w", err)
	}
	return newIdentityForHost(host)
}

func newIdentityForHost(host string) (*Identity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tunnel identity: generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("tunnel identity: serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("tunnel identity: create certificate: %w", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	spki, err := spkiSHA256(certDER)
	if err != nil {
		return nil, fmt.Errorf("tunnel identity: spki: %w", err)
	}

	return &Identity{
		Certificate: cert,
		SPKISHA256:  spki,
		TunnelHost:  host,
	}, nil
}

// randomTunnelHost returns a random 128-bit label. 25 characters from a
// 36-character alphabet yields ~129 bits of entropy.
func randomTunnelHost() (string, error) {
	const n = 25
	var b strings.Builder
	b.Grow(n)
	for range n {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(tunnelHostAlphabet))))
		if err != nil {
			return "", fmt.Errorf("random tunnel host: %w", err)
		}
		b.WriteByte(tunnelHostAlphabet[idx.Int64()])
	}
	return b.String(), nil
}

// spkiSHA256 computes the base64url SHA-256 of the SubjectPublicKeyInfo from a
// DER-encoded certificate.
func spkiSHA256(certDER []byte) (string, error) {
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return "", fmt.Errorf("parse certificate: %w", err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(pubBytes)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// PeerSPKISHA256 computes the base64url SHA-256 of the SubjectPublicKeyInfo
// from a verified peer certificate. It is used by both sides to compare against
// a pinned SPKI.
func PeerSPKISHA256(state tls.ConnectionState) (string, error) {
	if len(state.PeerCertificates) == 0 {
		return "", errors.New("no peer certificate")
	}
	return spkiSHA256(state.PeerCertificates[0].Raw)
}
