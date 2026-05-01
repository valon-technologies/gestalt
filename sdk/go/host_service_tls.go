package gestalt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// EnvHostServiceTLSCAFile points the SDK at an additional PEM CA bundle for
// tls:// host-service relay targets.
const EnvHostServiceTLSCAFile = "GESTALT_HOST_SERVICE_TLS_CA_FILE"

// EnvHostServiceTLSCAPEM provides additional PEM roots directly in the process
// environment for tls:// host-service relay targets.
const EnvHostServiceTLSCAPEM = "GESTALT_HOST_SERVICE_TLS_CA_PEM"

func hostServiceTLSConfig(serviceName, serverName string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: serverName,
		NextProtos: []string{"h2"},
	}
	pemBytes := []byte(strings.TrimSpace(os.Getenv(EnvHostServiceTLSCAPEM)))
	caFile := strings.TrimSpace(os.Getenv(EnvHostServiceTLSCAFile))
	if len(pemBytes) == 0 && caFile == "" {
		return cfg, nil
	}
	if len(pemBytes) == 0 {
		var err error
		pemBytes, err = os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("%s: read %s: %w", serviceName, EnvHostServiceTLSCAFile, err)
		}
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("%s: host service TLS CA did not contain any PEM certificates", serviceName)
	}
	cfg.RootCAs = roots
	return cfg, nil
}
