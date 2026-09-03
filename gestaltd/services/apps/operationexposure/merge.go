package operationexposure

// MergeAllowedOperationsWithOverlay combines the static deploy-config baseline
// with a runtime overlay. Overlay operations replace or add entries; Removed
// drops baseline keys before overrides are applied.
func MergeAllowedOperationsWithOverlay(
	static map[string]*OperationOverride,
	operations map[string]*OperationOverride,
	removed []string,
) map[string]*OperationOverride {
	if len(operations) == 0 && len(removed) == 0 {
		return static
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
