package appregistry_test

import (
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

func TestVersionDeploymentState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	policy := appregistry.RetentionPolicy{UnusedRetention: 72 * time.Hour, DeployedRetention: 720 * time.Hour}
	chain := appregistry.VersionDeploymentChain{
		Deployed: map[string]struct{}{"v-old": {}},
		Deadlines: map[string]time.Time{
			"v-old": now.Add(-10 * time.Hour),
		},
	}

	state, _ := appregistry.VersionDeploymentState("v-current", "v-current", now, policy, now, chain)
	if state != appregistry.DeploymentStateDesired {
		t.Fatalf("desired state = %q", state)
	}
	state, _ = appregistry.VersionDeploymentState("v-new", "v-current", now.Add(-96*time.Hour), policy, now, chain)
	if state != appregistry.DeploymentStateExpired {
		t.Fatalf("expired state = %q", state)
	}
	state, _ = appregistry.VersionDeploymentState("v-old", "v-current", now.Add(-800*time.Hour), policy, now, chain)
	if state != appregistry.DeploymentStateLocked {
		t.Fatalf("locked state = %q", state)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
