package workflowauth

import (
	"maps"
	"slices"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const (
	SubjectTypeApp        = "app"
	ResourceTypeOperation = "gestalt.workflow.operation"
	RelationInvoker       = "invoker"
	ActionInvoke          = "invoke"
)

const (
	OperationRunsStart         = "runs.start"
	OperationRunsList          = "runs.list"
	OperationRunsGet           = "runs.get"
	OperationRunsGetEvents     = "runs.getEvents"
	OperationRunsGetOutput     = "runs.getOutput"
	OperationRunsCancel        = "runs.cancel"
	OperationRunsSignal        = "runs.signal"
	OperationRunsSignalOrStart = "runs.signalOrStart"

	OperationEventsDeliver = "events.deliver"

	OperationDefinitionsApply               = "definitions.apply"
	OperationDefinitionsGet                 = "definitions.get"
	OperationDefinitionsList                = "definitions.list"
	OperationDefinitionsSetPaused           = "definitions.setPaused"
	OperationDefinitionsSetActivationPaused = "definitions.setActivationPaused"
	OperationDefinitionsDelete              = "definitions.delete"
)

var supportedOperations = map[string]struct{}{
	OperationRunsStart:         {},
	OperationRunsList:          {},
	OperationRunsGet:           {},
	OperationRunsGetEvents:     {},
	OperationRunsGetOutput:     {},
	OperationRunsCancel:        {},
	OperationRunsSignal:        {},
	OperationRunsSignalOrStart: {},

	OperationEventsDeliver: {},

	OperationDefinitionsApply:               {},
	OperationDefinitionsGet:                 {},
	OperationDefinitionsList:                {},
	OperationDefinitionsSetPaused:           {},
	OperationDefinitionsSetActivationPaused: {},
	OperationDefinitionsDelete:              {},
}

func IsSupportedOperation(operation string) bool {
	_, ok := supportedOperations[strings.TrimSpace(operation)]
	return ok
}

func SupportedOperations() []string {
	return slices.Sorted(maps.Keys(supportedOperations))
}

func OperationResourceID(appName, operation string) string {
	return strings.TrimSpace(appName) + "/operations/" + strings.TrimSpace(operation)
}

func OperationResourceType() *proto.AuthorizationModelResourceType {
	return &proto.AuthorizationModelResourceType{
		Name:        ResourceTypeOperation,
		SourceLayer: proto.SourceLayer_SOURCE_LAYER_STATIC_CONFIG,
		Relations: []*proto.ModelRelation{{
			Name: RelationInvoker,
			AllowedTargets: []*proto.ModelAllowedTarget{{
				Kind: &proto.ModelAllowedTarget_SubjectType{SubjectType: SubjectTypeApp},
			}},
		}},
		Actions: []*proto.ModelAction{{
			Name:      ActionInvoke,
			Relations: []string{RelationInvoker},
		}},
	}
}
