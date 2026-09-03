package operationexposure

import (
	"slices"
	"strings"
)

// MergeAllowedOperationsWithOverlay combines static deploy-config with a runtime overlay.
func MergeAllowedOperationsWithOverlay(
	static map[string]*OperationOverride,
	operations map[string]*OperationOverride,
	removed []string,
) map[string]*OperationOverride {
	if len(operations) == 0 && len(removed) == 0 {
		return cloneAllowedOperations(static)
	}
	merged := cloneAllowedOperations(static)
	for _, id := range removed {
		delete(merged, id)
	}
	for id, override := range operations {
		merged[id] = cloneOperationOverride(override)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func cloneAllowedOperations(in map[string]*OperationOverride) map[string]*OperationOverride {
	if in == nil {
		return make(map[string]*OperationOverride)
	}
	out := make(map[string]*OperationOverride, len(in))
	for id, override := range in {
		out[id] = cloneOperationOverride(override)
	}
	return out
}

func cloneOperationOverride(in *OperationOverride) *OperationOverride {
	if in == nil {
		return nil
	}
	out := *in
	if len(in.AllowedRoles) > 0 {
		out.AllowedRoles = append([]string(nil), in.AllowedRoles...)
	}
	if len(in.Tags) > 0 {
		out.Tags = append([]string(nil), in.Tags...)
	}
	return &out
}

func MergeOverlayPatch(
	currentOps map[string]*OperationOverride,
	currentRemoved []string,
	patchOps map[string]*OperationOverride,
	patchRemoved []string,
) (map[string]*OperationOverride, []string) {
	ops := cloneAllowedOperations(currentOps)
	removedSet := make(map[string]struct{}, len(currentRemoved)+len(patchRemoved))
	for _, id := range currentRemoved {
		id = strings.TrimSpace(id)
		if id != "" {
			removedSet[id] = struct{}{}
		}
	}
	for _, id := range patchRemoved {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		removedSet[id] = struct{}{}
		delete(ops, id)
	}
	for id, override := range patchOps {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ops[id] = cloneOperationOverride(override)
		delete(removedSet, id)
	}
	if len(ops) == 0 && len(removedSet) == 0 {
		return nil, nil
	}
	removed := make([]string, 0, len(removedSet))
	for id := range removedSet {
		removed = append(removed, id)
	}
	slices.Sort(removed)
	return ops, removed
}
