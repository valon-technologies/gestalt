package appregistry

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestPreviousVersionForInstall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing *core.AppInstallation
		want     string
	}{
		{
			name: "promoted",
			existing: &core.AppInstallation{
				RolloutStatus:   core.AppInstallationRolloutStatusPromoted,
				ResolvedVersion: "1.0.0",
			},
			want: "1.0.0",
		},
		{
			name: "failed_with_gold",
			existing: &core.AppInstallation{
				RolloutStatus:   core.AppInstallationRolloutStatusFailed,
				ResolvedVersion: "1.0.0",
			},
			want: "1.0.0",
		},
		{
			name: "pending_upgrade",
			existing: &core.AppInstallation{
				RolloutStatus:           core.AppInstallationRolloutStatusPending,
				ResolvedVersion:         "2.0.0",
				PreviousResolvedVersion: "1.0.0",
			},
			want: "1.0.0",
		},
		{
			name:     "nil",
			existing: nil,
			want:     "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := previousVersionForInstall(tc.existing); got != tc.want {
				t.Fatalf("previousVersionForInstall() = %q, want %q", got, tc.want)
			}
		})
	}
}
