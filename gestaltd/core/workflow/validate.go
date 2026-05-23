package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValueIsSet reports whether value selects any value kind.
func ValueIsSet(value Value) bool {
	switch {
	case value.LiteralSet:
		return true
	case value.Object != nil:
		return true
	case value.Array != nil:
		return true
	case value.Template != nil:
		return true
	case strings.TrimSpace(value.RunInput) != "":
		return true
	case strings.TrimSpace(value.SignalPayload) != "":
		return true
	case value.StepOutput != nil:
		return true
	default:
		return false
	}
}

// IsScalarJSON reports whether value is representable as a scalar JSON value.
func IsScalarJSON(value any) bool {
	switch value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

// ValidateValueMapRefs validates step-output references in a value map.
func ValidateValueMapRefs(path string, values map[string]Value, previousSteps map[string]struct{}) error {
	for key := range values {
		if err := ValidateValueRefs(path+"."+key, values[key], previousSteps); err != nil {
			return err
		}
	}
	return nil
}

// ValidateValueRefs validates step-output references in a value tree.
func ValidateValueRefs(path string, value Value, previousSteps map[string]struct{}) error {
	if value.StepOutput != nil {
		stepID := strings.TrimSpace(value.StepOutput.StepID)
		if stepID == "" {
			return fmt.Errorf("%s.step_output.step_id is required", path)
		}
		if _, ok := previousSteps[stepID]; !ok {
			return fmt.Errorf("%s.step_output.step_id %q must reference an earlier step", path, stepID)
		}
		if strings.TrimSpace(value.StepOutput.Path) == "" {
			return fmt.Errorf("%s.step_output.path is required", path)
		}
	}
	for key := range value.Object {
		if err := ValidateValueRefs(path+"."+key, value.Object[key], previousSteps); err != nil {
			return err
		}
	}
	for i := range value.Array {
		if err := ValidateValueRefs(fmt.Sprintf("%s[%d]", path, i), value.Array[i], previousSteps); err != nil {
			return err
		}
	}
	return nil
}

// ValidateStepWhen validates a step when clause and its value references.
func ValidateStepWhen(path string, when *StepWhen, previousSteps map[string]struct{}) error {
	if when == nil {
		return nil
	}
	if !ValueIsSet(when.Value) {
		return fmt.Errorf("%s.value is required", path)
	}
	if err := ValidateValueRefs(path+".value", when.Value, previousSteps); err != nil {
		return err
	}
	if !when.EqualsSet {
		return fmt.Errorf("%s.equals is required", path)
	}
	if !IsScalarJSON(when.Equals) {
		return fmt.Errorf("%s.equals must be a scalar JSON value", path)
	}
	return nil
}
