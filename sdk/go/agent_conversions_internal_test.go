package gestalt

import (
	"testing"

	proto "github.com/valon-technologies/gestalt/sdk/go/internal/gen/v1"
)

func TestAgentTurnEventsFromProtoSkipsNilEvents(t *testing.T) {
	events := agentTurnEventsFromProto([]*proto.AgentTurnEvent{
		nil,
		{
			Id:     "event-1",
			TurnId: "turn-1",
			Seq:    1,
			Type:   "message.delta",
		},
	})

	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(events), events)
	}
	if events[0].ID != "event-1" {
		t.Fatalf("event id = %q, want event-1", events[0].ID)
	}
}
