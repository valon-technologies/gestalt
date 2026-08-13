package server

import "testing"

func TestNoAuthConnectionIsNotProductConnected(t *testing.T) {
	t.Parallel()

	status := noAuthConnectionStatus()
	if status.Connected {
		t.Fatal("no-auth status must not mark a subject identity as connected")
	}
	if status.CredentialState != credentialStateNotRequired {
		t.Fatalf("credential state = %q, want %s", status.CredentialState, credentialStateNotRequired)
	}
}

func TestSummarizeReadyNoAuthDoesNotConnectTheApp(t *testing.T) {
	t.Parallel()

	got := summarizeConnectionStatuses([]connectionDefInfo{
		{
			Name:            "webhook",
			Status:          connectionStatusReady,
			CredentialState: credentialStateNotRequired,
			CredentialMode:  credentialModeNone,
			Connected:       false,
		},
		{
			Name:            "workspace",
			Status:          connectionStatusNeedsUserConnection,
			CredentialState: credentialStateMissing,
			CredentialMode:  credentialModeSubject,
			Connected:       false,
		},
	})
	if got.Connected {
		t.Fatal("a leftover no-auth row must not roll the app to connected")
	}
	if got.Status != connectionStatusNeedsUserConnection {
		t.Fatalf("status = %q, want %s", got.Status, connectionStatusNeedsUserConnection)
	}
}

func TestSummarizeAllReadyNoAuthIsNotProductConnected(t *testing.T) {
	t.Parallel()

	got := summarizeConnectionStatuses([]connectionDefInfo{{
		Name:            "webhook",
		Status:          connectionStatusReady,
		CredentialState: credentialStateNotRequired,
		CredentialMode:  credentialModeNone,
		Connected:       false,
	}})
	if got.Connected {
		t.Fatal("mode-none ready is not a chosen subject identity")
	}
}

func TestSummarizeAllReadySubjectStaysConnected(t *testing.T) {
	t.Parallel()

	got := summarizeConnectionStatuses([]connectionDefInfo{
		{
			Name:            "webhook",
			Status:          connectionStatusReady,
			CredentialState: credentialStateNotRequired,
			CredentialMode:  credentialModeNone,
			Connected:       false,
		},
		{
			Name:            "workspace",
			Status:          connectionStatusReady,
			CredentialState: credentialStateConnected,
			CredentialMode:  credentialModeSubject,
			Connected:       true,
		},
	})
	if !got.Connected {
		t.Fatal("a chosen subject identity must keep the app connected")
	}
}
