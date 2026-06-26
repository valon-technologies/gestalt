package daemon

import (
	"errors"
	"strings"
	"testing"
)

// The end-to-end publish flows live in the e2e package; this keeps the command
// runner's success and error branches covered in the fast unit suite.
func TestRunProviderPublishCommand(t *testing.T) {
	t.Parallel()

	out, err := runProviderPublishCommand("go", "version")
	if err != nil {
		t.Fatalf("runProviderPublishCommand(go version): %v", err)
	}
	if !strings.Contains(out, "go") {
		t.Fatalf("go version output = %q, want it to mention go", out)
	}

	_, err = runProviderPublishCommand("gestaltd-nonexistent-command-xyz")
	if err == nil {
		t.Fatal("runProviderPublishCommand with missing binary: expected error, got nil")
	}
	var cmdErr *providerPublishCommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error type = %T, want *providerPublishCommandError", err)
	}
}
