package gestalt

import (
	"strings"
	"testing"
)

func TestCursor_ContinueToKeyRejectsUnsupportedKey(t *testing.T) {
	cursor := &Cursor{}

	if cursor.ContinueToKey(make(chan int)) {
		t.Fatal("ContinueToKey returned true")
	}
	if cursor.Err() == nil {
		t.Fatal("Err() = nil, want conversion error")
	}
	if !strings.Contains(cursor.Err().Error(), "marshal") {
		t.Fatalf("Err() = %v, want marshal error", cursor.Err())
	}
}
