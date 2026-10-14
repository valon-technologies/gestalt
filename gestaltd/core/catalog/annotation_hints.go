package catalog

import "github.com/valon-technologies/gestalt/server/internal/ptrutil"

// CloneHintPointerFields deep-copies optional bool annotation pointers.
func CloneHintPointerFields(readOnly, idempotent, destructive, openWorld *bool) (readOnlyOut, idempotentOut, destructiveOut, openWorldOut *bool) {
	return ptrutil.Clone(readOnly), ptrutil.Clone(idempotent), ptrutil.Clone(destructive), ptrutil.Clone(openWorld)
}
