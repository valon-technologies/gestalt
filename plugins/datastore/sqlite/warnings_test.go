package sqlite

import (
	"strings"
	"testing"
)

func TestWarnings(t *testing.T) {
	t.Parallel()

	s := &Store{}
	warnings := s.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warnings))
	}
	if !strings.Contains(warnings[0], "not recommended for production") {
		t.Fatalf("unexpected warning: %s", warnings[0])
	}
}
