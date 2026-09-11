package appregistry

import (
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const processIDEnvVar = "GESTALTD_PROCESS_ID"

var (
	resolvedProcessID     string
	resolvedProcessIDOnce sync.Once
)

// ResolveProcessID returns a stable process identity for fleet readiness reports.
func ResolveProcessID() string {
	resolvedProcessIDOnce.Do(func() {
		resolvedProcessID = resolveProcessID(os.Getenv(processIDEnvVar))
	})
	return resolvedProcessID
}

func resolveProcessID(processIDEnv string) string {
	if v := strings.TrimSpace(processIDEnv); v != "" {
		return v
	}
	return uuid.NewString()
}
