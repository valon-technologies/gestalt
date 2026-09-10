package bootstrap

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

func TestPartitionPendingServingCritical(t *testing.T) {
	t.Parallel()

	entry := &config.ProviderEntry{Static: &config.AppStaticConfig{Mount: "/vds-forge"}}
	critical, other := partitionPendingServingCritical([]pendingProviderBuild{
		{name: "vds-forge", entry: entry},
		{name: "slack", entry: &config.ProviderEntry{}},
	})
	if len(critical) != 1 || critical[0].name != "vds-forge" {
		t.Fatalf("critical = %#v", critical)
	}
	if len(other) != 1 || other[0].name != "slack" {
		t.Fatalf("other = %#v", other)
	}
}
