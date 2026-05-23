package core

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type CapabilityAnnotations = catalog.CapabilityAnnotations

func CloneCapabilityAnnotations(a CapabilityAnnotations) CapabilityAnnotations {
	return catalog.CloneCapabilityAnnotations(a)
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
