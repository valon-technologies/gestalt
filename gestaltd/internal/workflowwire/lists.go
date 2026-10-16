package workflowwire

import "fmt"

func encodeWorkflowList[T, R any](values []T, label string, encode func(T) (R, error)) ([]R, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]R, 0, len(values))
	for i := range values {
		value, err := encode(values[i])
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", label, i, err)
		}
		out = append(out, value)
	}
	return out, nil
}

func decodeWorkflowList[T, R any](values []*T, decode func(*T) (R, error)) ([]R, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]R, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		decoded, err := decode(value)
		if err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}
