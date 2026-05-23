package catalog

// CloneHintPointerFields deep-copies optional bool annotation pointers.
func CloneHintPointerFields(readOnly, idempotent, destructive, openWorld *bool) (readOnlyOut, idempotentOut, destructiveOut, openWorldOut *bool) {
	if readOnly != nil {
		value := *readOnly
		readOnlyOut = &value
	}
	if idempotent != nil {
		value := *idempotent
		idempotentOut = &value
	}
	if destructive != nil {
		value := *destructive
		destructiveOut = &value
	}
	if openWorld != nil {
		value := *openWorld
		openWorldOut = &value
	}
	return readOnlyOut, idempotentOut, destructiveOut, openWorldOut
}
