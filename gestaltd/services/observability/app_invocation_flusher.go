package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AppInvocationFlusher publishes this process's request statistics to the
// shared store so any server can answer for the whole fleet.
type AppInvocationFlusher struct {
	Accumulator *AppInvocationAccumulator
	// Recent supplies each app's most recent invocations.
	Recent InvocationRecordReader
	Sink   AppInvocationStatsSink
	Writer AppInvocationWriter
	// Interval defaults to DefaultAppInvocationFlushInterval.
	Interval time.Duration
	// Retention defaults to DefaultAppInvocationRetention.
	Retention time.Duration
	Now       func() time.Time

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}
	passMu    sync.Mutex
	nextPrune time.Time
}

func (f *AppInvocationFlusher) interval() time.Duration {
	if f.Interval > 0 {
		return f.Interval
	}
	return DefaultAppInvocationFlushInterval
}

func (f *AppInvocationFlusher) retention() time.Duration {
	if f.Retention > 0 {
		return f.Retention
	}
	return DefaultAppInvocationRetention
}

func (f *AppInvocationFlusher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Start begins flushing on an interval until ctx is cancelled or Stop is called.
func (f *AppInvocationFlusher) Start(ctx context.Context) {
	if f == nil || f.Accumulator == nil || f.Sink == nil {
		return
	}
	f.startOnce.Do(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		f.cancel = cancel
		f.done = make(chan struct{})
		go func() {
			defer close(f.done)
			ticker := time.NewTicker(f.interval())
			defer ticker.Stop()
			for {
				select {
				case <-loopCtx.Done():
					return
				case <-ticker.C:
					f.flushAndLog(loopCtx)
				}
			}
		}()
	})
}

// Stop ends the loop and publishes whatever is still pending.
func (f *AppInvocationFlusher) Stop() {
	if f == nil {
		return
	}
	f.stopOnce.Do(func() {
		if f.cancel != nil {
			f.cancel()
			<-f.done
		}
		ctx, cancel := context.WithTimeout(context.Background(), f.interval())
		defer cancel()
		f.flushAndLog(ctx)
	})
}

func (f *AppInvocationFlusher) flushAndLog(ctx context.Context) {
	if err := f.FlushOnce(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("app invocation stats flush failed", "instance_id", f.Writer.InstanceID, "error", err)
	}
}

// FlushOnce publishes the changes since the last successful flush. A failed
// flush leaves the changes pending, and because rows hold cumulative values
// the next flush repairs it without double counting.
func (f *AppInvocationFlusher) FlushOnce(ctx context.Context) error {
	if f == nil || f.Accumulator == nil || f.Sink == nil {
		return fmt.Errorf("app invocation flusher is not configured")
	}
	f.passMu.Lock()
	defer f.passMu.Unlock()

	pending := f.Accumulator.Pending()
	if len(pending.Buckets) > 0 {
		if err := f.Sink.WriteBuckets(ctx, f.Writer, pending.Buckets); err != nil {
			return fmt.Errorf("write buckets: %w", err)
		}
	}
	if f.Recent != nil {
		for _, provider := range pending.RecentProviders {
			records := f.Recent.RecentInvocations(provider, DefaultAppInvocationRecentLimit)
			if err := f.Sink.WriteRecent(ctx, f.Writer, provider, records); err != nil {
				return fmt.Errorf("write recent invocations for %q: %w", provider, err)
			}
		}
	}
	f.Accumulator.Commit(pending)

	now := f.now()
	if f.nextPrune.IsZero() || !now.Before(f.nextPrune) {
		f.nextPrune = now.Add(f.retention() / 4)
		if err := f.Sink.PruneBefore(ctx, now.Add(-f.retention())); err != nil {
			return fmt.Errorf("prune: %w", err)
		}
	}
	return nil
}
