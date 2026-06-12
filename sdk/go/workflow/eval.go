package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
)

// ErrInvalidValue reports a workflow value expression that cannot be
// evaluated against its run context.
var ErrInvalidValue = errors.New("invalid workflow value")

// EvalContext carries runtime inputs for workflow value and template evaluation.
type EvalContext struct {
	Request    Request
	Outputs    map[string]any
	StepInputs map[string]any
}

// EvaluateStepInputs resolves a step's workflow value expressions against
// the run context, prior step outputs, and step inputs.
func (c EvalContext) EvaluateStepInputs(values map[string]gestalt.WorkflowValue) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(values))
	for key := range values {
		resolved, ok, err := c.EvaluateValue(values[key])
		if err != nil {
			return nil, fmt.Errorf("inputs.%s: %w", key, err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: inputs.%s did not resolve", ErrInvalidValue, key)
		}
		out[key] = resolved
	}
	return out, nil
}

// EvaluateValue resolves one workflow value expression against the run
// context.
func (c EvalContext) EvaluateValue(value gestalt.WorkflowValue) (any, bool, error) {
	switch {
	case value.LiteralSet:
		return value.Literal, true, nil
	case value.Object != nil:
		out := make(map[string]any, len(value.Object))
		for key := range value.Object {
			resolved, ok, err := c.EvaluateValue(value.Object[key])
			if err != nil {
				return nil, false, fmt.Errorf("%s: %w", key, err)
			}
			if !ok {
				return nil, false, nil
			}
			out[key] = resolved
		}
		return out, true, nil
	case value.Array != nil:
		out := make([]any, 0, len(value.Array))
		for i := range value.Array {
			resolved, ok, err := c.EvaluateValue(value.Array[i])
			if err != nil {
				return nil, false, fmt.Errorf("[%d]: %w", i, err)
			}
			if !ok {
				return nil, false, nil
			}
			out = append(out, resolved)
		}
		return out, true, nil
	case value.Template != nil:
		rendered, err := c.RenderTemplate(value.Template.Template)
		return rendered, err == nil, err
	case strings.TrimSpace(value.Input) != "":
		return MapPathValue(c.Request.Input, value.Input)
	case strings.TrimSpace(value.Signal) != "":
		signal := LatestSignal(c.Request.Signals)
		if signal == nil {
			return nil, false, nil
		}
		return PathValue(signal.Payload, value.Signal)
	case value.StepOutput != nil:
		stepID := strings.TrimSpace(value.StepOutput.StepID)
		output, ok := c.Outputs[stepID]
		if !ok {
			return nil, false, fmt.Errorf("%w: workflow step output references missing step %q", ErrInvalidValue, stepID)
		}
		return PathValue(output, value.StepOutput.Path)
	case value.StepInput != nil:
		stepID := strings.TrimSpace(value.StepInput.StepID)
		input, ok := c.StepInputs[stepID]
		if !ok {
			return nil, false, fmt.Errorf("%w: workflow step input references missing step %q", ErrInvalidValue, stepID)
		}
		return PathValue(input, value.StepInput.Path)
	default:
		return nil, true, nil
	}
}

// RenderTemplate renders a workflow text template against the run context.
func (c EvalContext) RenderTemplate(template string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(template); {
		if strings.HasPrefix(template[i:], "$${{") {
			b.WriteString("${{")
			i += 4
			continue
		}
		if !strings.HasPrefix(template[i:], "${{") {
			b.WriteByte(template[i])
			i++
			continue
		}
		end := strings.Index(template[i+3:], "}}")
		if end < 0 {
			return "", fmt.Errorf("%w: unterminated template expression", ErrInvalidValue)
		}
		expr := strings.TrimSpace(template[i+3 : i+3+end])
		value, ok, err := c.templateExpressionValue(expr)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("%w: template expression %q did not resolve", ErrInvalidValue, expr)
		}
		rendered, err := templateRenderValue(value)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
		i += 3 + end + 2
	}
	return b.String(), nil
}

func (c EvalContext) templateExpressionValue(expr string) (any, bool, error) {
	switch {
	case strings.HasPrefix(expr, "input."):
		return MapPathValue(c.Request.Input, strings.TrimPrefix(expr, "input."))
	case expr == "input":
		return c.Request.Input, true, nil
	case strings.HasPrefix(expr, "signal."):
		signal := LatestSignal(c.Request.Signals)
		if signal == nil {
			return nil, false, nil
		}
		return PathValue(signal.Payload, strings.TrimPrefix(expr, "signal."))
	case expr == "signal":
		signal := LatestSignal(c.Request.Signals)
		if signal == nil {
			return nil, false, nil
		}
		return signal.Payload, true, nil
	case strings.HasPrefix(expr, "steps."):
		return c.workflowStepExpressionValue(strings.TrimPrefix(expr, "steps."))
	default:
		return nil, false, fmt.Errorf("%w: unsupported template expression %q", ErrInvalidValue, expr)
	}
}

func (c EvalContext) workflowStepExpressionValue(expr string) (any, bool, error) {
	stepID, tail, ok := strings.Cut(expr, ".")
	if !ok || strings.TrimSpace(stepID) == "" {
		return nil, false, fmt.Errorf("%w: invalid step expression %q", ErrInvalidValue, expr)
	}
	kind, path, _ := strings.Cut(tail, ".")
	switch kind {
	case "outputs":
		output, exists := c.Outputs[stepID]
		if !exists {
			return nil, false, fmt.Errorf("%w: workflow step output references missing step %q", ErrInvalidValue, stepID)
		}
		return PathValue(output, path)
	case "inputs":
		input, exists := c.StepInputs[stepID]
		if !exists {
			return nil, false, fmt.Errorf("%w: workflow step input references missing step %q", ErrInvalidValue, stepID)
		}
		return PathValue(input, path)
	default:
		return nil, false, fmt.Errorf("%w: unsupported step expression %q", ErrInvalidValue, expr)
	}
}

func templateRenderValue(value any) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// LatestSignal returns the most recent delivery of a named signal.
func LatestSignal(signals []gestalt.WorkflowSignal) *gestalt.WorkflowSignal {
	if len(signals) == 0 {
		return nil
	}
	return &signals[len(signals)-1]
}

// MapPathValue reads a dotted path from a map value.
func MapPathValue(values map[string]any, path string) (any, bool, error) {
	if len(values) == 0 {
		return nil, false, nil
	}
	return PathValue(values, path)
}

// PathValue reads a dotted path from any JSON-like value.
func PathValue(root any, path string) (any, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return root, true, nil
	}
	segments, err := pathSegments(path)
	if err != nil {
		return nil, false, err
	}
	current := root
	for _, segment := range segments {
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[segment.key]
			if !ok {
				return nil, false, nil
			}
			current = next
		case []any:
			if !segment.indexSet || segment.index < 0 || segment.index >= len(typed) {
				return nil, false, nil
			}
			current = typed[segment.index]
		default:
			return nil, false, nil
		}
	}
	return current, true, nil
}

type pathSegment struct {
	key      string
	index    int
	indexSet bool
}

func pathSegments(path string) ([]pathSegment, error) {
	var out []pathSegment
	for i := 0; i < len(path); {
		switch path[i] {
		case '.':
			i++
			continue
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("%w: invalid workflow path %q", ErrInvalidValue, path)
			}
			token := strings.TrimSpace(path[i+1 : i+end])
			if strings.HasPrefix(token, "'") || strings.HasPrefix(token, "\"") {
				unquoted, err := unquotePathKey(token)
				if err != nil {
					return nil, fmt.Errorf("%w: invalid workflow path %q", ErrInvalidValue, path)
				}
				out = append(out, pathSegment{key: unquoted})
			} else {
				index, err := strconv.Atoi(token)
				if err != nil {
					return nil, fmt.Errorf("%w: invalid workflow path %q", ErrInvalidValue, path)
				}
				out = append(out, pathSegment{index: index, indexSet: true})
			}
			i += end + 1
		default:
			start := i
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				i++
			}
			key := strings.TrimSpace(path[start:i])
			if key == "" {
				return nil, fmt.Errorf("%w: invalid workflow path %q", ErrInvalidValue, path)
			}
			out = append(out, pathSegment{key: key})
		}
	}
	return out, nil
}

func unquotePathKey(token string) (string, error) {
	if strings.HasPrefix(token, "\"") {
		return strconv.Unquote(token)
	}
	if len(token) < 2 || token[len(token)-1] != '\'' {
		return "", strconv.ErrSyntax
	}
	remaining := token[1 : len(token)-1]
	var out strings.Builder
	for len(remaining) > 0 {
		value, _, tail, err := strconv.UnquoteChar(remaining, '\'')
		if err != nil {
			return "", err
		}
		out.WriteRune(value)
		remaining = tail
	}
	return out.String(), nil
}

// IsScalarJSON reports whether a value is a JSON scalar.
func IsScalarJSON(value any) bool {
	switch value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

// ScalarEqual compares two JSON scalars for equality, treating integral
// floats and ints as equal.
func ScalarEqual(left, right any) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if leftString, ok := left.(string); ok {
		rightString, ok := right.(string)
		return ok && leftString == rightString
	}
	if leftBool, ok := left.(bool); ok {
		rightBool, ok := right.(bool)
		return ok && leftBool == rightBool
	}
	leftNumber, leftOK := scalarNumber(left)
	rightNumber, rightOK := scalarNumber(right)
	return leftOK && rightOK && leftNumber == rightNumber
}

func scalarNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
