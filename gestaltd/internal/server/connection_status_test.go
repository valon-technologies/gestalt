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

func TestOwnerKindForNilPrincipalIsUnknown(t *testing.T) {
	t.Parallel()
	if got := ownerKindForPrincipal(nil); got != ownerKindUnknown {
		t.Fatalf("owner kind = %q, want %s", got, ownerKindUnknown)
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

func TestSummarizeReconnectKeepsAppConnectedWhenSiblingIsValid(t *testing.T) {
	t.Parallel()

	got, ok := summarizeReconnectRequiredConnectionStatuses([]connectionDefInfo{
		{
			Name:            "workspace",
			Status:          connectionStatusReady,
			CredentialState: credentialStateConnected,
			CredentialMode:  credentialModeSubject,
			Connected:       true,
		},
		{
			Name:            "archive",
			Status:          connectionStatusNeedsUserConnection,
			CredentialState: credentialStateInvalid,
			CredentialMode:  credentialModeSubject,
			StatusCode:      "reconnect_required",
			Connected:       false,
		},
	})
	if !ok {
		t.Fatal("expected reconnect rollup for mixed valid and stale connections")
	}
	if !got.Connected {
		t.Fatal("a chosen account must keep the app connected when a sibling needs reconnect")
	}
	if got.Status != connectionStatusDegraded {
		t.Fatalf("status = %q, want %s", got.Status, connectionStatusDegraded)
	}
}

func TestSummarizeReconnectNoAuthDoesNotConnectTheApp(t *testing.T) {
	t.Parallel()

	got, ok := summarizeReconnectRequiredConnectionStatuses([]connectionDefInfo{
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
			CredentialState: credentialStateInvalid,
			CredentialMode:  credentialModeSubject,
			StatusCode:      "reconnect_required",
			Connected:       false,
		},
	})
	if !ok {
		t.Fatal("expected reconnect rollup when a subject connection is invalid")
	}
	if got.Connected {
		t.Fatal("mode-none ready must not mark the app connected during reconnect rollup")
	}
}

func TestDefaultModeNoneDoesNotHideSubjectProductConnected(t *testing.T) {
	t.Parallel()

	s := &Server{defaultConnection: map[string]string{"demo": "webhook"}}
	info := &integrationInfo{
		Name: "demo",
		Connections: []connectionDefInfo{
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
		},
	}
	s.applyIntegrationConnectionStatus(info, nil, nil, nil, nil)
	if !info.Connected {
		t.Fatal("chosen subject account must keep the app product-connected when the default row is mode-none")
	}
}

func TestDefaultModeNoneAloneIsNotProductConnected(t *testing.T) {
	t.Parallel()

	s := &Server{defaultConnection: map[string]string{"demo": "webhook"}}
	info := &integrationInfo{
		Name: "demo",
		Connections: []connectionDefInfo{{
			Name:            "webhook",
			Status:          connectionStatusReady,
			CredentialState: credentialStateNotRequired,
			CredentialMode:  credentialModeNone,
			Connected:       false,
		}},
	}
	s.applyIntegrationConnectionStatus(info, nil, nil, nil, nil)
	if info.Connected {
		t.Fatal("a mode-none default row must not mark the app product-connected")
	}
}

func TestMarkPreferredInstances_MarksInvalidChosenAccount(t *testing.T) {
	t.Parallel()

	got := markPreferredInstances([]instanceInfo{
		{Name: "default", credentialInvalid: true},
	}, "default")
	if len(got) != 1 || !got[0].Preferred {
		t.Fatalf("preferred = %+v, want chosen invalid account marked preferred", got)
	}
}

func TestMarkPreferredInstances_IgnoresStalePreference(t *testing.T) {
	t.Parallel()

	got := markPreferredInstances([]instanceInfo{
		{Name: "default"},
	}, "gone")
	if len(got) != 1 || got[0].Preferred {
		t.Fatalf("preferred = %+v, stale preference must not mark an instance", got)
	}
}

func TestPreferredInstanceValid_RejectsInvalidGrant(t *testing.T) {
	t.Parallel()

	if preferredInstanceValid([]instanceInfo{
		{Name: "default", credentialInvalid: true},
	}, "default") {
		t.Fatal("invalid chosen account is not a valid acting account")
	}
}
