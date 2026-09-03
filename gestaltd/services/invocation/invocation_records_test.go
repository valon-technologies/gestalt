package invocation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/services/observability"
)

type recordingInvocationRecorder struct {
	records []observability.InvocationRecord
}

func (r *recordingInvocationRecorder) RecordInvocation(record observability.InvocationRecord) {
	r.records = append(r.records, record)
}

func TestBrokerRecordsCompletedInvocation(t *testing.T) {
	t.Parallel()

	recorder := &recordingInvocationRecorder{}
	broker := &Broker{invocationRecorder: recorder}
	startedAt := time.Now().Add(-time.Millisecond)
	broker.recordCompletedInvocation(context.Background(), startedAt, "g-issues", "list", "http", "subject", 200, false)

	if len(recorder.records) != 1 {
		t.Fatalf("got %d records, want 1", len(recorder.records))
	}
	record := recorder.records[0]
	if record.Provider != "g-issues" || record.Operation != "list" || record.Outcome != observability.InvocationPassed || record.Status != 200 {
		t.Fatalf("record = %#v", record)
	}
	if record.Duration <= 0 || record.Timestamp.IsZero() {
		t.Fatalf("record timing = %#v", record)
	}
}

func TestObservingStreamReaderRecordsOnceAtTerminalCompletion(t *testing.T) {
	t.Parallel()

	recorder := &recordingInvocationRecorder{}
	broker := &Broker{invocationRecorder: recorder}
	startedAt := time.Now().Add(-time.Millisecond)
	_, span := broker.tracer().Start(context.Background(), "test")
	reader := &observingStreamReader{
		inner:        core.StreamReaderFunc(streamFrames([]*core.InvokeFrame{{Metadata: &core.InvokeMetadata{Status: 200}}})),
		broker:       broker,
		ctx:          context.Background(),
		span:         span,
		startedAt:    startedAt,
		providerName: "g-issues",
		operation:    "stream",
	}

	if _, err := reader.Recv(); err != nil {
		t.Fatalf("first Recv: %v", err)
	}
	if _, err := reader.Recv(); err != io.EOF {
		t.Fatalf("second Recv error = %v, want EOF", err)
	}
	if _, err := reader.Recv(); err != io.EOF {
		t.Fatalf("third Recv error = %v, want EOF", err)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("got %d records, want exactly 1", len(recorder.records))
	}
	if recorder.records[0].Outcome != observability.InvocationPassed || recorder.records[0].Status != 200 {
		t.Fatalf("record = %#v", recorder.records[0])
	}
}

func TestObservingStreamReaderRecordsTrailingFailureBeforeReturn(t *testing.T) {
	recorder := &recordingInvocationRecorder{}
	broker := &Broker{invocationRecorder: recorder}
	_, span := broker.tracer().Start(context.Background(), "test")
	reader := &observingStreamReader{
		inner: core.StreamReaderFunc(streamFrames([]*core.InvokeFrame{
			{Metadata: &core.InvokeMetadata{Status: http.StatusOK}},
			{Data: []byte("chunk")},
			{Metadata: &core.InvokeMetadata{Status: http.StatusBadGateway}, Data: []byte("error")},
		})),
		broker:       broker,
		ctx:          context.Background(),
		span:         span,
		startedAt:    time.Now().Add(-time.Millisecond),
		providerName: "g-issues",
		operation:    "stream",
	}

	for i := 0; i < 3; i++ {
		if _, err := reader.Recv(); err != nil {
			t.Fatalf("Recv %d: %v", i, err)
		}
	}
	if len(recorder.records) != 1 {
		t.Fatalf("got %d records, want exactly 1", len(recorder.records))
	}
	if got := recorder.records[0]; got.Outcome != observability.InvocationFailed || got.Status != http.StatusBadGateway {
		t.Fatalf("record = %#v, want failed 502", got)
	}
}

func TestObservingStreamReaderMapsPreMetadataErrorStatus(t *testing.T) {
	recorder := &recordingInvocationRecorder{}
	broker := &Broker{invocationRecorder: recorder}
	_, span := broker.tracer().Start(context.Background(), "test")
	streamErr := errors.New("upstream stream failed")
	reader := &observingStreamReader{
		inner: core.StreamReaderFunc(func() (*core.InvokeFrame, error) {
			return nil, streamErr
		}),
		broker:       broker,
		ctx:          context.Background(),
		span:         span,
		startedAt:    time.Now().Add(-time.Millisecond),
		providerName: "g-issues",
		operation:    "stream",
	}

	if _, err := reader.Recv(); !errors.Is(err, streamErr) {
		t.Fatalf("Recv error = %v, want %v", err, streamErr)
	}
	if len(recorder.records) != 1 {
		t.Fatalf("got %d records, want exactly 1", len(recorder.records))
	}
	if got := recorder.records[0]; got.Outcome != observability.InvocationFailed || got.Status != http.StatusBadGateway {
		t.Fatalf("record = %#v, want failed 502", got)
	}
}

func streamFrames(frames []*core.InvokeFrame) func() (*core.InvokeFrame, error) {
	index := 0
	return func() (*core.InvokeFrame, error) {
		if index == len(frames) {
			return nil, io.EOF
		}
		frame := frames[index]
		index++
		return frame, nil
	}
}
