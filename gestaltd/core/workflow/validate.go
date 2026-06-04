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
	case strings.TrimSpace(value.Input) != "":
		return true
	case strings.TrimSpace(value.Signal) != "":
		return true
	case value.StepOutput != nil:
		return true
	case value.StepInput != nil:
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

// ValidateValueMapRefs validates step references in a value map.
func ValidateValueMapRefs(path string, values map[string]Value, previousSteps map[string]struct{}) error {
	for key := range values {
		if err := ValidateValueRefs(path+"."+key, values[key], previousSteps); err != nil {
			return err
		}
	}
	return nil
}

// ValidateValueRefs validates step references in a value tree.
func ValidateValueRefs(path string, value Value, previousSteps map[string]struct{}) error {
	if value.Template != nil {
		if err := ValidateTemplateRefs(path+".template", value.Template.Template, previousSteps); err != nil {
			return err
		}
	}
	if value.StepOutput != nil {
		stepID := strings.TrimSpace(value.StepOutput.StepID)
		if stepID == "" {
			return fmt.Errorf("%s.step_output.step_id is required", path)
		}
		if _, ok := previousSteps[stepID]; !ok {
			return fmt.Errorf("%s.step_output.step_id %q must reference an earlier step", path, stepID)
		}
	}
	if value.StepInput != nil {
		stepID := strings.TrimSpace(value.StepInput.StepID)
		if stepID == "" {
			return fmt.Errorf("%s.step_input.step_id is required", path)
		}
		if _, ok := previousSteps[stepID]; !ok {
			return fmt.Errorf("%s.step_input.step_id %q must reference an earlier step", path, stepID)
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

// TemplateExpressions returns the unescaped ${{ ... }} expressions in a
// workflow template. A leading extra dollar escapes the marker.
func TemplateExpressions(template string) ([]string, error) {
	var out []string
	for i := 0; i < len(template); {
		if strings.HasPrefix(template[i:], "$${{") {
			i += 4
			continue
		}
		if !strings.HasPrefix(template[i:], "${{") {
			i++
			continue
		}
		end := strings.Index(template[i+3:], "}}")
		if end < 0 {
			return nil, fmt.Errorf("unterminated template expression")
		}
		expr := strings.TrimSpace(template[i+3 : i+3+end])
		if expr == "" {
			return nil, fmt.Errorf("empty template expression")
		}
		out = append(out, expr)
		i += 3 + end + 2
	}
	return out, nil
}

// ValidateTemplateRefs validates workflow template expression roots and step
// references without evaluating the template.
func ValidateTemplateRefs(path string, template string, previousSteps map[string]struct{}) error {
	expressions, err := TemplateExpressions(template)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, expr := range expressions {
		if err := validateTemplateExpressionRef(expr, previousSteps); err != nil {
			return fmt.Errorf("%s expression %q: %w", path, expr, err)
		}
	}
	return nil
}

func validateTemplateExpressionRef(expr string, previousSteps map[string]struct{}) error {
	switch {
	case strings.HasPrefix(expr, "input."):
		if strings.TrimSpace(strings.TrimPrefix(expr, "input.")) == "" {
			return fmt.Errorf("input path is required")
		}
		return nil
	case strings.HasPrefix(expr, "signal."):
		if strings.TrimSpace(strings.TrimPrefix(expr, "signal.")) == "" {
			return fmt.Errorf("signal path is required")
		}
		return nil
	case strings.HasPrefix(expr, "steps."):
		return validateTemplateStepExpression(strings.TrimPrefix(expr, "steps."), previousSteps)
	default:
		return fmt.Errorf("unsupported root; expected input.*, signal.*, steps.<step>.outputs, or steps.<step>.inputs")
	}
}

func validateTemplateStepExpression(expr string, previousSteps map[string]struct{}) error {
	stepID, tail, ok := strings.Cut(expr, ".")
	stepID = strings.TrimSpace(stepID)
	if !ok || stepID == "" {
		return fmt.Errorf("step id is required")
	}
	if _, ok := previousSteps[stepID]; !ok {
		return fmt.Errorf("step %q must reference an earlier step", stepID)
	}
	switch {
	case tail == "outputs":
		return nil
	case strings.HasPrefix(tail, "outputs."):
		if strings.TrimSpace(strings.TrimPrefix(tail, "outputs.")) == "" {
			return fmt.Errorf("step output path is required")
		}
		return nil
	case tail == "inputs":
		return nil
	case strings.HasPrefix(tail, "inputs."):
		if strings.TrimSpace(strings.TrimPrefix(tail, "inputs.")) == "" {
			return fmt.Errorf("step input path is required")
		}
		return nil
	default:
		return fmt.Errorf("expected outputs or inputs")
	}
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
