package core

import "encoding/json"

type CapabilityAnnotations struct {
	ReadOnlyHint    *bool
	IdempotentHint  *bool
	DestructiveHint *bool
	OpenWorldHint   *bool
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
