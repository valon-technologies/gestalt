package workflowwire

import (
	"fmt"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
)

// ParseValue converts a JSON-shaped value into core workflow value form.
func ParseValue(value any, path string) (coreworkflow.Value, error) {
	if value == nil {
		return coreworkflow.Value{}, nil
	}
	if valueMap, ok := asMap(value); ok {
		if len(valueMap) == 1 {
			for key, raw := range valueMap {
				switch key {
				case "literal":
					return coreworkflow.Value{Literal: CloneJSON(raw), LiteralSet: true}, nil
				case "object":
					objectValue, err := parseValueMap(raw, path+".object")
					return coreworkflow.Value{Object: objectValue}, err
				case "array":
					arrayValue, err := parseValueArray(raw, path+".array")
					return coreworkflow.Value{Array: arrayValue}, err
				case "template":
					text, err := parseText(raw, path+".template")
					if err != nil {
						return coreworkflow.Value{}, err
					}
					return coreworkflow.Value{Template: &text}, nil
				case "runInput":
					return coreworkflow.Value{RunInput: stringValue(raw)}, nil
				case "signalPayload":
					return coreworkflow.Value{SignalPayload: stringValue(raw)}, nil
				case "stepOutput":
					stepOutputMap, ok := asMap(raw)
					if !ok {
						return coreworkflow.Value{}, fmt.Errorf("%w: %s.stepOutput must be an object", ErrInvalid, path)
					}
					if err := rejectUnknownKeys(stepOutputMap, path+".stepOutput", "stepId", "path"); err != nil {
						return coreworkflow.Value{}, err
					}
					return coreworkflow.Value{StepOutput: &coreworkflow.StepOutputSource{
						StepID: stringArg(stepOutputMap, "stepId"),
						Path:   stringArg(stepOutputMap, "path"),
					}}, nil
				}
			}
		}
		objectValue, err := parseValueMap(valueMap, path)
		return coreworkflow.Value{Object: objectValue}, err
	}
	if arrayValue, ok := value.([]any); ok {
		out := make([]coreworkflow.Value, 0, len(arrayValue))
		for i, item := range arrayValue {
			converted, err := ParseValue(item, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return coreworkflow.Value{}, err
			}
			out = append(out, converted)
		}
		return coreworkflow.Value{Array: out}, nil
	}
	return coreworkflow.Value{Literal: CloneJSON(value), LiteralSet: true}, nil
}

func parseValueMap(value any, path string) (map[string]coreworkflow.Value, error) {
	if value == nil {
		return nil, nil
	}
	valueMap, ok := asMap(value)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", ErrInvalid, path)
	}
	out := make(map[string]coreworkflow.Value, len(valueMap))
	for key, raw := range valueMap {
		converted, err := ParseValue(raw, path+"."+key)
		if err != nil {
			return nil, err
		}
		out[key] = converted
	}
	return out, nil
}

func parseValueArray(value any, path string) ([]coreworkflow.Value, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array", ErrInvalid, path)
	}
	out := make([]coreworkflow.Value, 0, len(items))
	for i, item := range items {
		converted, err := ParseValue(item, fmt.Sprintf("%s[%d]", path, i))
		if err != nil {
			return nil, err
		}
		out = append(out, converted)
	}
	return out, nil
}

func parseText(value any, path string) (coreworkflow.Text, error) {
	if value == nil {
		return coreworkflow.Text{}, nil
	}
	if text, ok := value.(string); ok {
		return coreworkflow.Text{Template: strings.TrimSpace(text)}, nil
	}
	textMap, ok := asMap(value)
	if !ok {
		return coreworkflow.Text{}, fmt.Errorf("%w: %s must be a string or object", ErrInvalid, path)
	}
	if err := rejectUnknownKeys(textMap, path, "template"); err != nil {
		return coreworkflow.Text{}, err
	}
	return coreworkflow.Text{Template: stringArg(textMap, "template")}, nil
}

// EncodeValue converts a core workflow value into canonical JSON shape.
func EncodeValue(value coreworkflow.Value) any {
	switch {
	case value.LiteralSet:
		return map[string]any{"literal": CloneJSON(value.Literal)}
	case value.Object != nil:
		return map[string]any{"object": encodeValueMap(value.Object)}
	case value.Array != nil:
		items := make([]any, 0, len(value.Array))
		for i := range value.Array {
			items = append(items, EncodeValue(value.Array[i]))
		}
		return map[string]any{"array": items}
	case value.Template != nil:
		return encodeText(*value.Template)
	case strings.TrimSpace(value.RunInput) != "":
		return map[string]any{"runInput": value.RunInput}
	case strings.TrimSpace(value.SignalPayload) != "":
		return map[string]any{"signalPayload": value.SignalPayload}
	case value.StepOutput != nil:
		return map[string]any{"stepOutput": map[string]any{
			"stepId": value.StepOutput.StepID,
			"path":   value.StepOutput.Path,
		}}
	default:
		return nil
	}
}

func encodeValueMap(values map[string]coreworkflow.Value) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key := range values {
		out[key] = EncodeValue(values[key])
	}
	return out
}

func encodeText(text coreworkflow.Text) map[string]any {
	if text.Template == "" {
		return nil
	}
	return map[string]any{"template": text.Template}
}
