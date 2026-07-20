package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	coreworkflow "github.com/valon-technologies/gestalt/server/core/workflow"
	"github.com/valon-technologies/gestalt/server/internal/workflowwire"
	"github.com/valon-technologies/gestalt/server/services/identity/principal"
)

type workflowTextRequest struct {
	Template string `json:"template,omitempty"`
}

func (r *workflowTextRequest) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		r.Template = text
		return nil
	}
	type alias workflowTextRequest
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*r = workflowTextRequest(out)
	return nil
}

type workflowStepOutputSourceRequest struct {
	StepID string `json:"stepId,omitempty"`
	Path   string `json:"path,omitempty"`
}

type workflowValueRequest struct {
	Literal    any
	LiteralSet bool
	Object     map[string]workflowValueRequest
	ObjectSet  bool
	Array      []workflowValueRequest
	ArraySet   bool
	Template   *workflowTextRequest
	Input      string
	Signal     string
	StepOutput *workflowStepOutputSourceRequest
	StepInput  *workflowStepOutputSourceRequest
}

func (r *workflowValueRequest) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		r.Literal = nil
		r.LiteralSet = true
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil {
		if len(object) == 1 {
			for key, raw := range object {
				switch key {
				case "literal":
					if err := json.Unmarshal(raw, &r.Literal); err != nil {
						return err
					}
					r.LiteralSet = true
					return nil
				case "object":
					if err := json.Unmarshal(raw, &r.Object); err != nil {
						return err
					}
					r.ObjectSet = true
					return nil
				case "array":
					if err := json.Unmarshal(raw, &r.Array); err != nil {
						return err
					}
					r.ArraySet = true
					return nil
				case "template":
					var text workflowTextRequest
					if err := json.Unmarshal(raw, &text); err != nil {
						return err
					}
					r.Template = &text
					return nil
				case "input":
					return json.Unmarshal(raw, &r.Input)
				case "signal":
					return json.Unmarshal(raw, &r.Signal)
				case "stepOutput":
					var source workflowStepOutputSourceRequest
					if err := json.Unmarshal(raw, &source); err != nil {
						return err
					}
					r.StepOutput = &source
					return nil
				case "stepInput":
					var source workflowStepOutputSourceRequest
					if err := json.Unmarshal(raw, &source); err != nil {
						return err
					}
					r.StepInput = &source
					return nil
				}
			}
		}
		values := make(map[string]workflowValueRequest, len(object))
		for key, raw := range object {
			var value workflowValueRequest
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			values[key] = value
		}
		r.Object = values
		r.ObjectSet = true
		return nil
	}
	var array []workflowValueRequest
	if err := json.Unmarshal(data, &array); err == nil {
		r.Array = array
		r.ArraySet = true
		return nil
	}
	var literal any
	if err := json.Unmarshal(data, &literal); err != nil {
		return err
	}
	r.Literal = literal
	r.LiteralSet = true
	return nil
}

type workflowTextInfo struct {
	Template string `json:"template,omitempty"`
}

type workflowStepWhenInfo struct {
	Value     any
	Equals    any
	EqualsSet bool
}

func (i workflowStepWhenInfo) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if i.Value != nil {
		out["value"] = i.Value
	}
	if i.EqualsSet {
		out["equals"] = i.Equals
	}
	return json.Marshal(out)
}

func (s *Server) resolveWorkflowActor(w http.ResponseWriter, r *http.Request) (*principal.Principal, bool) {
	p := principal.Canonicalized(PrincipalFromContext(r.Context()))
	if p == nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return nil, false
	}
	if strings.TrimSpace(p.SubjectID) == "" {
		writeError(w, http.StatusUnauthorized, "missing subject")
		return nil, false
	}
	return p, true
}

func workflowTextFromRequest(text workflowTextRequest) coreworkflow.Text {
	return coreworkflow.Text{Template: strings.TrimSpace(text.Template)}
}

func workflowValueObjectMapFromRequest(values map[string]workflowValueRequest) map[string]coreworkflow.Value {
	out := make(map[string]coreworkflow.Value, len(values))
	for key := range values {
		out[key] = workflowValueFromRequest(values[key])
	}
	return out
}

func workflowValueListFromRequest(values []workflowValueRequest) []coreworkflow.Value {
	out := make([]coreworkflow.Value, 0, len(values))
	for i := range values {
		out = append(out, workflowValueFromRequest(values[i]))
	}
	return out
}

func workflowValueFromRequest(value workflowValueRequest) coreworkflow.Value {
	out := coreworkflow.Value{
		Literal:    value.Literal,
		LiteralSet: value.LiteralSet,
		Input:      strings.TrimSpace(value.Input),
		Signal:     strings.TrimSpace(value.Signal),
	}
	if value.ObjectSet {
		out.Object = workflowValueObjectMapFromRequest(value.Object)
	}
	if value.ArraySet {
		out.Array = workflowValueListFromRequest(value.Array)
	}
	if value.Template != nil {
		text := workflowTextFromRequest(*value.Template)
		out.Template = &text
	}
	if value.StepOutput != nil {
		out.StepOutput = &coreworkflow.StepOutputSource{StepID: strings.TrimSpace(value.StepOutput.StepID), Path: strings.TrimSpace(value.StepOutput.Path)}
	}
	if value.StepInput != nil {
		out.StepInput = &coreworkflow.StepInputSource{StepID: strings.TrimSpace(value.StepInput.StepID), Path: strings.TrimSpace(value.StepInput.Path)}
	}
	return out
}

func workflowStepWhenInfoFromCore(when *coreworkflow.StepWhen) *workflowStepWhenInfo {
	if when == nil {
		return nil
	}
	return &workflowStepWhenInfo{
		Value:     workflowValueInfoFromCore(when.Value),
		Equals:    when.Equals,
		EqualsSet: when.EqualsSet,
	}
}

func workflowTextInfoFromCore(text coreworkflow.Text) *workflowTextInfo {
	if strings.TrimSpace(text.Template) == "" {
		return nil
	}
	return &workflowTextInfo{Template: text.Template}
}

func workflowValueObjectInfoFromCore(values map[string]coreworkflow.Value) map[string]any {
	out := make(map[string]any, len(values))
	for key := range values {
		out[key] = workflowValueInfoFromCore(values[key])
	}
	return out
}

func workflowValueInfoFromCore(value coreworkflow.Value) any {
	switch {
	case value.LiteralSet:
		return map[string]any{"literal": workflowwire.CloneJSON(value.Literal)}
	case value.Object != nil:
		return map[string]any{"object": workflowValueObjectInfoFromCore(value.Object)}
	case value.Array != nil:
		items := make([]any, 0, len(value.Array))
		for i := range value.Array {
			items = append(items, workflowValueInfoFromCore(value.Array[i]))
		}
		return map[string]any{"array": items}
	case value.Template != nil:
		return map[string]any{"template": workflowTextInfoFromCore(*value.Template)}
	case strings.TrimSpace(value.Input) != "":
		return map[string]any{"input": value.Input}
	case strings.TrimSpace(value.Signal) != "":
		return map[string]any{"signal": value.Signal}
	case value.StepOutput != nil:
		return map[string]any{"stepOutput": map[string]any{
			"stepId": value.StepOutput.StepID,
			"path":   value.StepOutput.Path,
		}}
	case value.StepInput != nil:
		return map[string]any{"stepInput": map[string]any{
			"stepId": value.StepInput.StepID,
			"path":   value.StepInput.Path,
		}}
	default:
		return nil
	}
}
