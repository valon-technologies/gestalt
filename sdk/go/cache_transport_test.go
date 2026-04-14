package gestalt_test

import (
	"context"
	"testing"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

func TestTransportCacheNamedSocketEnv(t *testing.T) {
	client, err := gestalt.Cache("test")
	if err != nil {
		t.Fatalf("connect named cache: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Set(ctx, "named", []byte("ok"), gestalt.CacheSetOptions{}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, found, err := client.Get(ctx, "named")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found || string(value) != "ok" {
		t.Fatalf("Get = (%q, %v), want (%q, true)", value, found, "ok")
	}
}

func TestTransportCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	if err := testCacheClient.SetMany(ctx, []gestalt.CacheEntry{
		{Key: "alpha", Value: []byte("one")},
		{Key: "beta", Value: []byte("two")},
	}, gestalt.CacheSetOptions{TTL: time.Minute}); err != nil {
		t.Fatalf("SetMany: %v", err)
	}

	values, err := testCacheClient.GetMany(ctx, []string{"alpha", "beta", "missing"})
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if got := string(values["alpha"]); got != "one" {
		t.Fatalf(`GetMany["alpha"] = %q, want %q`, got, "one")
	}
	if got := string(values["beta"]); got != "two" {
		t.Fatalf(`GetMany["beta"] = %q, want %q`, got, "two")
	}
	if _, ok := values["missing"]; ok {
		t.Fatal(`GetMany["missing"] should be absent`)
	}

	touched, err := testCacheClient.Touch(ctx, "alpha", time.Minute)
	if err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if !touched {
		t.Fatal("Touch returned false, want true")
	}

	deleted, err := testCacheClient.Delete(ctx, "alpha")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted {
		t.Fatal("Delete returned false, want true")
	}

	deletedCount, err := testCacheClient.DeleteMany(ctx, []string{"beta", "missing"})
	if err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if deletedCount != 1 {
		t.Fatalf("DeleteMany deleted = %d, want 1", deletedCount)
	}
}
