package server

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestTokenExpiresIn(t *testing.T) {
	t.Parallel()

	t.Run("nil omits hint", func(t *testing.T) {
		t.Parallel()
		got, err := tokenExpiresIn(nil)
		if err != nil || got != 0 {
			t.Fatalf("tokenExpiresIn(nil) = (%d, %v), want (0, nil)", got, err)
		}
	})

	t.Run("zero omits hint", func(t *testing.T) {
		t.Parallel()
		zero := int64(0)
		got, err := tokenExpiresIn(&zero)
		if err != nil || got != 0 {
			t.Fatalf("tokenExpiresIn(0) = (%d, %v), want (0, nil)", got, err)
		}
	})

	t.Run("positive forwards seconds", func(t *testing.T) {
		t.Parallel()
		for _, want := range []int64{30 * 24 * 3600, 90 * 24 * 3600, 365 * 24 * 3600} {
			value := want
			got, err := tokenExpiresIn(&value)
			if err != nil || got != want {
				t.Fatalf("tokenExpiresIn(%d) = (%d, %v), want (%d, nil)", want, got, err, want)
			}
		}
	})

	t.Run("negative rejects", func(t *testing.T) {
		t.Parallel()
		negative := int64(-1)
		if _, err := tokenExpiresIn(&negative); err == nil {
			t.Fatal("tokenExpiresIn(-1) error = nil, want rejection")
		}
	})

	t.Run("over max rejects", func(t *testing.T) {
		t.Parallel()
		over := core.MaxTokenExpiresInSeconds + 1
		if _, err := tokenExpiresIn(&over); err == nil {
			t.Fatal("tokenExpiresIn(over max) error = nil, want rejection")
		}
	})
}
