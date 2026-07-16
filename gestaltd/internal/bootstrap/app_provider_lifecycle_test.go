package bootstrap

import (
	"context"
	"testing"
	"time"
)

func TestAppProviderLifecycleSerializesOneApp(t *testing.T) {
	t.Parallel()

	lifecycles := &appProviderLifecycles{}
	releaseFirst, err := lifecycles.acquire(context.Background(), "g-issues")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	acquiredSecond := make(chan func(), 1)
	go func() {
		release, acquireErr := lifecycles.acquire(context.Background(), "g-issues")
		if acquireErr == nil {
			acquiredSecond <- release
		}
	}()

	select {
	case <-acquiredSecond:
		t.Fatal("second lifecycle acquired before first released")
	case <-time.After(25 * time.Millisecond):
	}
	releaseFirst()

	select {
	case releaseSecond := <-acquiredSecond:
		releaseSecond()
	case <-time.After(time.Second):
		t.Fatal("second lifecycle did not acquire after release")
	}
}
