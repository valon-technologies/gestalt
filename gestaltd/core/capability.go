package core

import "encoding/json"

type CapabilityAnnotations struct {
	ReadOnlyHint    *bool
	IdempotentHint  *bool
	DestructiveHint *bool
	OpenWorldHint   *bool
}

func CloneCapabilityAnnotations(a CapabilityAnnotations) CapabilityAnnotations {
	out := CapabilityAnnotations{}
	if a.ReadOnlyHint != nil {
		value := *a.ReadOnlyHint
		out.ReadOnlyHint = &value
	}
	if a.IdempotentHint != nil {
		value := *a.IdempotentHint
		out.IdempotentHint = &value
	}
	if a.DestructiveHint != nil {
		value := *a.DestructiveHint
		out.DestructiveHint = &value
	}
	if a.OpenWorldHint != nil {
		value := *a.OpenWorldHint
		out.OpenWorldHint = &value
	}
	return out
}

type Capability struct {
	Provider    string
	Operation   string
	Title       string
	Description string
	Parameters  []Parameter
	InputSchema json.RawMessage
	Method      string
	Transport   string
	Annotations CapabilityAnnotations
}
