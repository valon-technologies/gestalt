package sqlite

import (
	"fmt"
	"testing"
)

func TestWarnings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "relative path",
			path: "./gestalt.db",
			want: `sqlite datastore path "./gestalt.db" uses local filesystem storage; in a container this path is ephemeral. Use an absolute path on a mounted persistent volume or switch to a shared datastore such as postgres.`,
		},
		{
			name: "temp path",
			path: "/tmp/gestalt.db",
			want: `sqlite datastore path "/tmp/gestalt.db" uses temporary storage; data will be lost on restart. Use a mounted persistent volume or switch to a shared datastore such as postgres.`,
		},
		{
			name: "var tmp",
			path: "/var/tmp/gestalt.db",
			want: `sqlite datastore path "/var/tmp/gestalt.db" uses temporary storage; data will be lost on restart. Use a mounted persistent volume or switch to a shared datastore such as postgres.`,
		},
		{
			name: "mounted absolute path",
			path: "/data/gestalt.db",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &Store{path: tc.path}
			warnings := s.Warnings()

			if tc.want == "" {
				if len(warnings) != 0 {
					t.Fatalf("got %v, want none", warnings)
				}
				return
			}
			if len(warnings) != 1 {
				t.Fatalf("got %d warnings, want 1", len(warnings))
			}
			if warnings[0] != tc.want {
				fmt.Println("got: ", warnings[0])
				fmt.Println("want:", tc.want)
				t.Fatal("warning mismatch")
			}
		})
	}
}
