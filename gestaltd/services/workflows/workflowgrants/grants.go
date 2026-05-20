package workflowgrants

import (
	"maps"
	"slices"
	"strings"
)

const (
	OperationRunsStart         = "runs.start"
	OperationRunsSignal        = "runs.signal"
	OperationRunsSignalOrStart = "runs.signalOrStart"
	OperationRunsCancel        = "runs.cancel"

	OperationDeploymentsCreate = "deployments.create"
	OperationDeploymentsGet    = "deployments.get"
	OperationDeploymentsDelete = "deployments.delete"
	OperationDeploymentsPause  = "deployments.pause"
	OperationDeploymentsResume = "deployments.resume"

	OperationSchedulesCreate = "schedules.create"
	OperationSchedulesGet    = "schedules.get"
	OperationSchedulesUpdate = "schedules.update"
	OperationSchedulesDelete = "schedules.delete"
	OperationSchedulesPause  = "schedules.pause"
	OperationSchedulesResume = "schedules.resume"

	OperationEventTriggersCreate = "eventTriggers.create"
	OperationEventTriggersGet    = "eventTriggers.get"
	OperationEventTriggersUpdate = "eventTriggers.update"
	OperationEventTriggersDelete = "eventTriggers.delete"
	OperationEventTriggersPause  = "eventTriggers.pause"
	OperationEventTriggersResume = "eventTriggers.resume"

	OperationEventsPublish = "events.publish"

	OperationDefinitionsCreate = "definitions.create"
	OperationDefinitionsGet    = "definitions.get"
	OperationDefinitionsUpdate = "definitions.update"
	OperationDefinitionsDelete = "definitions.delete"
)

type Grants map[string]struct{}

var supportedOperations = map[string]struct{}{
	OperationRunsStart:         {},
	OperationRunsSignal:        {},
	OperationRunsSignalOrStart: {},
	OperationRunsCancel:        {},

	OperationDeploymentsCreate: {},
	OperationDeploymentsGet:    {},
	OperationDeploymentsDelete: {},
	OperationDeploymentsPause:  {},
	OperationDeploymentsResume: {},

	OperationSchedulesCreate: {},
	OperationSchedulesGet:    {},
	OperationSchedulesUpdate: {},
	OperationSchedulesDelete: {},
	OperationSchedulesPause:  {},
	OperationSchedulesResume: {},

	OperationEventTriggersCreate: {},
	OperationEventTriggersGet:    {},
	OperationEventTriggersUpdate: {},
	OperationEventTriggersDelete: {},
	OperationEventTriggersPause:  {},
	OperationEventTriggersResume: {},

	OperationEventsPublish: {},

	OperationDefinitionsCreate: {},
	OperationDefinitionsGet:    {},
	OperationDefinitionsUpdate: {},
	OperationDefinitionsDelete: {},
}

func IsSupportedOperation(operation string) bool {
	_, ok := supportedOperations[strings.TrimSpace(operation)]
	return ok
}

func SupportedOperations() []string {
	return slices.Sorted(maps.Keys(supportedOperations))
}

func Clone(src Grants) Grants {
	if src == nil {
		return nil
	}
	return maps.Clone(src)
}

func EncodeClaims(src Grants) []string {
	if src == nil {
		return nil
	}
	if len(src) == 0 {
		return []string{}
	}
	return slices.Sorted(maps.Keys(src))
}

func DecodeClaims(src []string) Grants {
	if src == nil {
		return nil
	}
	out := make(Grants, len(src))
	for _, operation := range src {
		operation = strings.TrimSpace(operation)
		if operation == "" {
			continue
		}
		out[operation] = struct{}{}
	}
	return out
}

func (g Grants) Allows(operation string) bool {
	if g == nil {
		return true
	}
	_, ok := g[strings.TrimSpace(operation)]
	return ok
}
