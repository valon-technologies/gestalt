package appregistry

import (
	"testing"
	"time"
)

func TestMaterializerLocksOnlyMatchingAppVersion(t *testing.T) {
	t.Parallel()

	materializer := &Materializer{}
	unlockFirst := materializer.lockMaterialization("app-a\x001.0.0")
	firstLocked := true
	t.Cleanup(func() {
		if firstLocked {
			unlockFirst()
		}
	})

	differentAcquired := make(chan func(), 1)
	go func() {
		differentAcquired <- materializer.lockMaterialization("app-b\x001.0.0")
	}()
	select {
	case unlock := <-differentAcquired:
		unlock()
	case <-time.After(time.Second):
		t.Fatal("different app version was blocked")
	}

	sameAcquired := make(chan func(), 1)
	go func() {
		sameAcquired <- materializer.lockMaterialization("app-a\x001.0.0")
	}()
	select {
	case unlock := <-sameAcquired:
		unlock()
		t.Fatal("matching app version was not serialized")
	case <-time.After(20 * time.Millisecond):
	}

	unlockFirst()
	firstLocked = false
	select {
	case unlock := <-sameAcquired:
		unlock()
	case <-time.After(time.Second):
		t.Fatal("matching app version remained blocked after unlock")
	}
}
