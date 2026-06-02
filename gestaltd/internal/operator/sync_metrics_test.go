package operator

import (
	"fmt"
	"testing"
	"time"
)

func TestSyncMetricsRecorderKeepsOnlySlowestFetches(t *testing.T) {
	t.Parallel()

	recorder := NewSyncMetricsRecorder()
	for i := 0; i < syncMetricsMaxSlowFetches+3; i++ {
		recorder.RecordArchiveFetch(syncArchiveMetricsEvent{
			Subject:  fmt.Sprintf("fetch-%d", i),
			Duration: time.Duration(i) * time.Second,
		})
	}

	got := recorder.Snapshot().Archives.SlowestFetches
	if len(got) != syncMetricsMaxSlowFetches {
		t.Fatalf("slowest fetches len = %d, want %d", len(got), syncMetricsMaxSlowFetches)
	}
	for i, fetch := range got {
		want := fmt.Sprintf("fetch-%d", syncMetricsMaxSlowFetches+2-i)
		if fetch.Subject != want {
			t.Fatalf("slowest fetches[%d].subject = %q, want %q", i, fetch.Subject, want)
		}
	}
}
