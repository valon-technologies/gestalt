package core

import (
	"encoding/json"

	"github.com/valon-technologies/gestalt/server/core/catalog"
)

type CapabilityAnnotations struct {
	ReadOnlyHint    *bool
	IdempotentHint  *bool
	DestructiveHint *bool
	OpenWorldHint   *bool
}

func CloneCapabilityAnnotations(a CapabilityAnnotations) CapabilityAnnotations {
	readOnly, idempotent, destructive, openWorld := catalog.CloneHintPointerFields(
		a.ReadOnlyHint,
		a.IdempotentHint,
		a.DestructiveHint,
		a.OpenWorldHint,
	)
	return CapabilityAnnotations{
		ReadOnlyHint:    readOnly,
		IdempotentHint:  idempotent,
		DestructiveHint: destructive,
		OpenWorldHint:   openWorld,
	}
}

func CapabilityAnnotationsFromCatalog(a catalog.CapabilityAnnotations) CapabilityAnnotations {
	return CapabilityAnnotations{
		ReadOnlyHint:    a.ReadOnlyHint,
		IdempotentHint:  a.IdempotentHint,
		DestructiveHint: a.DestructiveHint,
		OpenWorldHint:   a.OpenWorldHint,
	}
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
