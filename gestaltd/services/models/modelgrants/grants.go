package modelgrants

import (
	"maps"
	"slices"
	"strings"
)

const OperationGenerate = "model.generate"

type Grants map[string]struct{}

var supportedOperations = map[string]struct{}{
	OperationGenerate: {},
}

func IsSupportedOperation(operation string) bool {
	_, ok := supportedOperations[strings.TrimSpace(operation)]
	return ok
}

func SupportedOperations() []string {
	return slices.Sorted(maps.Keys(supportedOperations))
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
