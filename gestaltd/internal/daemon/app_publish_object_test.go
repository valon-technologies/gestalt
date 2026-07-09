package daemon

import (
	"encoding/json"
	"testing"
)

func TestGCSObjectGenerationUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "numeric", input: `123`, want: 123},
		{name: "string", input: `"456"`, want: 456},
		{name: "null", input: `null`, want: 0},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var described appPublishObjectDescription
			if err := json.Unmarshal([]byte(`{"generation":`+tc.input+`}`), &described); err != nil {
				if !tc.wantErr {
					t.Fatalf("UnmarshalJSON: %v", err)
				}
				return
			}
			if tc.wantErr {
				t.Fatal("expected error")
			}
			if int64(described.Generation) != tc.want {
				t.Fatalf("generation = %d, want %d", described.Generation, tc.want)
			}
		})
	}
}
