package e2e

// Local mirrors of unexported daemon symbols the black-box e2e suite needs to
// decode the gestaltd binary's JSON output. Keep JSON tags in sync with daemon.

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/operator"
)

// mirrors daemon.syncOutputDocument (sync_output.go). Only the fields asserted
// by the e2e tests are required; unknown JSON fields are ignored on decode.
type syncOutputDocument struct {
	Command string `json:"command"`
	operator.SyncMetrics
}

// mirrors daemon.provider_publish.go publish-plan types and constants.
const (
	providerPublishPlanSchema      = "gestaltd.provider.publish.plan.v1"
	providerPublishFileKindArchive = "archive"
)

type providerPublishFile struct {
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`
	LocalPath  string `json:"localPath"`
	StorageURL string `json:"storageUrl"`
	PublicURL  string `json:"publicUrl"`
	SHA256     string `json:"sha256"`
}

type providerPublishPlan struct {
	Schema            string                `json:"schema"`
	PublishRepository string                `json:"publishRepository"`
	SourceRepository  string                `json:"sourceRepository"`
	SourceRef         string                `json:"sourceRef"`
	ProviderDir       string                `json:"providerDir"`
	ManifestPath      string                `json:"manifestPath"`
	Version           string                `json:"version"`
	Metadata          providerPublishFile   `json:"metadata"`
	Artifacts         []providerPublishFile `json:"artifacts"`
	Files             []providerPublishFile `json:"files"`
}

// mirrors daemon.defaultReleaseOutputDir (provider_release_types.go).
const defaultReleaseOutputDir = "dist/"

// runProviderPublishCommand runs a helper subprocess (e.g. git) for publish-test
// setup; a faithful copy of the daemon helper of the same name.
func runProviderPublishCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s failed: %w\n%s%s", name, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}
