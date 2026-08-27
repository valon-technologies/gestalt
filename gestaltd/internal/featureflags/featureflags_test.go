package featureflags

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cloud.google.com/go/storage"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	readErr := errors.New("permission denied")
	tests := []struct {
		name    string
		values  map[string]string
		errors  map[string]error
		want    Snapshot
		wantErr string
	}{
		{name: "missing objects are disabled"},
		{name: "parses values", values: map[string]string{"agent": "true\n", "workflow": "false"}, want: Snapshot{Agent: true}},
		{name: "rejects invalid values", values: map[string]string{"agent": "TRUE"}, wantErr: "content must be true or false"},
		{name: "returns read errors", errors: map[string]error{"agent": readErr}, wantErr: readErr.Error()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := load(context.Background(), "flags", func(_ context.Context, _, object string) ([]byte, error) {
				if err := test.errors[object]; err != nil {
					return nil, err
				}
				if value, ok := test.values[object]; ok {
					return []byte(value), nil
				}
				return nil, storage.ErrObjectNotExist
			})
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("load error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got.Enabled(Agent) != test.want.Enabled(Agent) || got.Enabled(Workflow) != test.want.Enabled(Workflow) {
				t.Fatalf("load = %#v, want %#v", got, test.want)
			}
		})
	}
}
