package invocationconfig

import (
	"testing"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/services/workflows/workflowgrants"
)

func TestAppWorkflowManagerGrantsRequireExplicitWorkflowCapabilities(t *testing.T) {
	t.Parallel()

	for name, capabilities := range map[string]*config.AppCapabilitiesConfig{
		"nil capabilities":      nil,
		"omitted workflow caps": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			grants := AppWorkflowManagerGrants(capabilities)
			if grants == nil {
				t.Fatal("grants = nil, want explicit deny-all grants")
			}
			if grants.Allows(workflowgrants.OperationEventsPublish) {
				t.Fatalf("grants allow %q, want denied", workflowgrants.OperationEventsPublish)
			}
		})
	}

	grants := AppWorkflowManagerGrants(&config.AppCapabilitiesConfig{
		Workflow: &config.AppWorkflowCapabilitiesConfig{
			Operations: []string{workflowgrants.OperationEventsPublish},
		},
	})
	if !grants.Allows(workflowgrants.OperationEventsPublish) {
		t.Fatalf("grants deny %q, want allowed", workflowgrants.OperationEventsPublish)
	}
}
