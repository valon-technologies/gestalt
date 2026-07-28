package server

import (
	"context"
	"testing"
)

func TestResolveRevisionActorLabelPreservesSystemActor(t *testing.T) {
	t.Parallel()

	server := &Server{}
	if got := server.resolveRevisionActorLabel(context.Background(), "system:auto-deploy"); got != "system:auto-deploy" {
		t.Fatalf("system actor label = %q, want system:auto-deploy", got)
	}
}
