package featureflags

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubObjectReader struct {
	values map[string][]byte
	errors map[string]error
}

func (r stubObjectReader) ReadObject(_ context.Context, _, object string) ([]byte, error) {
	if err := r.errors[object]; err != nil {
		return nil, err
	}
	if value, ok := r.values[object]; ok {
		return value, nil
	}
	return nil, storage.ErrObjectNotExist
}

func TestLoadUsesDeclaredDefaultsForMissingObjects(t *testing.T) {
	snapshot, err := load(context.Background(), "flags", stubObjectReader{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if snapshot.Enabled(Agent) || snapshot.Enabled(Workflow) {
		t.Fatalf("snapshot = %#v, want all flags disabled", snapshot.Values())
	}
}

func TestLoadParsesStrictBooleanContent(t *testing.T) {
	snapshot, err := load(context.Background(), "flags", stubObjectReader{values: map[string][]byte{
		"agent":    []byte(" true\n"),
		"workflow": []byte("false"),
	}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !snapshot.Enabled(Agent) || snapshot.Enabled(Workflow) {
		t.Fatalf("snapshot = %#v", snapshot.Values())
	}
}

func TestLoadRejectsInvalidContent(t *testing.T) {
	_, err := load(context.Background(), "flags", stubObjectReader{values: map[string][]byte{"agent": []byte("TRUE")}})
	if err == nil || !strings.Contains(err.Error(), "content must be exactly true or false") {
		t.Fatalf("load error = %v", err)
	}
}

func TestLoadRejectsOversizedContent(t *testing.T) {
	_, err := load(context.Background(), "flags", stubObjectReader{values: map[string][]byte{"agent": []byte(strings.Repeat("x", maxFlagObjectBytes+1))}})
	if err == nil || !strings.Contains(err.Error(), "object exceeds") {
		t.Fatalf("load error = %v", err)
	}
}

func TestLoadReturnsReadErrors(t *testing.T) {
	wantErr := errors.New("permission denied")
	_, err := load(context.Background(), "flags", stubObjectReader{errors: map[string]error{"agent": wantErr}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("load error = %v, want %v", err, wantErr)
	}
}

func TestDisabledErrorCarriesGRPCStatus(t *testing.T) {
	err := NewDisabledError(Agent)
	if !errors.Is(err, ErrDisabled) || !IsDisabled(err, Agent) || IsDisabled(err, Workflow) {
		t.Fatalf("unexpected disabled error identity: %v", err)
	}
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("status code = %v, want %v", got, codes.FailedPrecondition)
	}
	if got := err.Error(); got != "agent feature is not enabled" {
		t.Fatalf("error = %q", got)
	}
}
