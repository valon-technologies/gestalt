package workflowgrants

import (
	"maps"
	"slices"
	"strings"
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

type Grants map[string]struct{}

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

func All() Grants {
	return maps.Clone(supportedOperations)
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
		return false
	}
	_, ok := g[strings.TrimSpace(operation)]
	return ok
}
