package server

import (
	"fmt"
	"testing"
	"time"
)

func TestLoadAdminRegistryAppSummariesUsesBoundedConcurrencyAndPreservesOrder(t *testing.T) {
	t.Parallel()

	apps := make([]configuredRegistryApp, adminRegistrySummaryConcurrency+1)
	for index := range apps {
		apps[index] = configuredRegistryApp{name: fmt.Sprintf("app-%02d", index)}
	}
	started := make(chan string, len(apps))
	release := make(chan struct{})
	type result struct {
		summaries []adminRegistryAppSummary
		err       error
	}
	done := make(chan result, 1)
	go func() {
		summaries, err := loadAdminRegistryAppSummaries(apps, func(app configuredRegistryApp) (adminRegistryAppSummary, error) {
			started <- app.name
			<-release
			return adminRegistryAppSummary{App: app.name}, nil
		})
		done <- result{summaries: summaries, err: err}
	}()

	for range adminRegistrySummaryConcurrency {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for concurrent summary load")
		}
	}
	select {
	case app := <-started:
		t.Fatalf("summary load exceeded concurrency limit with %q", app)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)

	got := <-done
	if got.err != nil {
		t.Fatalf("load summaries: %v", got.err)
	}
	for index, summary := range got.summaries {
		if summary.App != apps[index].name {
			t.Fatalf("summary %d = %q, want %q", index, summary.App, apps[index].name)
		}
	}
}
