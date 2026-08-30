package config

import (
	"strings"
	"testing"
)

func TestValidateProviderSnapshotRepositoryAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		repo    ProviderSnapshotRepositoryConfig
		wantErr string
	}{
		{
			name: "GCP ADC on Google Cloud Storage",
			repo: ProviderSnapshotRepositoryConfig{
				URL:  "https://storage.googleapis.com/private-snapshots",
				Auth: ProviderSnapshotRepositoryAuthGCPADC,
			},
		},
		{
			name: "legacy unauthenticated HTTP repository",
			repo: ProviderSnapshotRepositoryConfig{URL: "http://snapshots.example.test"},
		},
		{
			name: "unsupported auth",
			repo: ProviderSnapshotRepositoryConfig{
				URL:  "https://storage.googleapis.com/private-snapshots",
				Auth: "other",
			},
			wantErr: `auth must be "gcpADC" when set`,
		},
		{
			name: "GCP ADC on another host",
			repo: ProviderSnapshotRepositoryConfig{
				URL:  "https://snapshots.example.test",
				Auth: ProviderSnapshotRepositoryAuthGCPADC,
			},
			wantErr: `requires an https://storage.googleapis.com URL`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{ProviderSnapshotRepositories: map[string]ProviderSnapshotRepositoryConfig{"valon": test.repo}}
			err := validateProviderSnapshotRepositories(cfg)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validateProviderSnapshotRepositories() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateProviderSnapshotRepositories() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}
