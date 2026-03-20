package testutil

import "testing"

func CloseOnCleanup(t *testing.T, c interface{ Close() }) {
	t.Helper()
	t.Cleanup(c.Close)
}
