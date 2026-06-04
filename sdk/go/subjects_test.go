package gestalt

import "testing"

func TestParseSubjectID(t *testing.T) {
	t.Parallel()

	kind, id, ok := ParseSubjectID("user:ada")
	if !ok || kind != "user" || id != "ada" {
		t.Fatalf("ParseSubjectID(user:ada) = (%q, %q, %v), want (user, ada, true)", kind, id, ok)
	}

	if _, _, ok := ParseSubjectID("invalid"); ok {
		t.Fatalf("ParseSubjectID(invalid) = ok, want false")
	}
}
