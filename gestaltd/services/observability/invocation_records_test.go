package observability

import (
	"testing"
	"time"
)

func TestInvocationRecordStoreZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var store InvocationRecordStore

	store.RecordInvocation(InvocationRecord{
		Provider:  "g-issues",
		Operation: "list",
		Outcome:   InvocationPassed,
	})

	records := store.RecentInvocations("g-issues", 1)
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one record", records)
	}
	if records[0].ID != 1 || records[0].Provider != "g-issues" || records[0].Operation != "list" {
		t.Fatalf("record = %#v", records[0])
	}
}

func TestInvocationRecordStoreReturnsNewestRecordsPerProvider(t *testing.T) {
	t.Parallel()

	store := NewInvocationRecordStore(2)
	startedAt := time.Date(2026, time.September, 3, 12, 0, 0, 123000000, time.UTC)
	store.RecordInvocation(InvocationRecord{Provider: "g-issues", Operation: "list", Outcome: InvocationPassed, Timestamp: startedAt})
	store.RecordInvocation(InvocationRecord{Provider: "slack", Operation: "post", Outcome: InvocationFailed, Timestamp: startedAt.Add(time.Second)})
	store.RecordInvocation(InvocationRecord{Provider: "g-issues", Operation: "update", Outcome: InvocationFailed, Timestamp: startedAt.Add(2 * time.Second)})
	store.RecordInvocation(InvocationRecord{Provider: "g-issues", Operation: "delete", Outcome: InvocationPassed, Timestamp: startedAt.Add(3 * time.Second)})

	got := store.RecentInvocations("g-issues", 32)
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2: %#v", len(got), got)
	}
	if got[0].Operation != "delete" || got[0].Outcome != InvocationPassed || got[0].ID != 4 {
		t.Fatalf("newest record = %#v", got[0])
	}
	if got[1].Operation != "update" || got[1].ID != 3 {
		t.Fatalf("oldest retained record = %#v", got[1])
	}
	if len(store.RecentInvocations("slack", 32)) != 1 {
		t.Fatal("records from another provider were evicted")
	}
}
