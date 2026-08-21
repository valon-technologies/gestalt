package providermanifestv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const SourceRunRoleUI = "ui"

// SourcePhaseCommand is one serial step in an install, build, or run phase.
type SourcePhaseCommand struct {
	Command      []string          `json:"command" yaml:"command"`
	Workdir      string            `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Env          map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Inputs       []string          `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	ReadyTimeout string            `json:"readyTimeout,omitempty" yaml:"readyTimeout,omitempty"`
	Role         string            `json:"role,omitempty" yaml:"role,omitempty"`
}

type phaseWireForm int

const (
	phaseWireUnset phaseWireForm = iota
	phaseWireLegacyObject
	phaseWirePrepareOnlySequence
	phaseWireCommandList
)

type sourcePhaseCommandWire struct {
	Command      []string          `json:"command" yaml:"command"`
	Workdir      string            `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Env          map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Inputs       []string          `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	ReadyTimeout string            `json:"readyTimeout,omitempty" yaml:"readyTimeout,omitempty"`
	Role         string            `json:"role,omitempty" yaml:"role,omitempty"`
}

func validateSourceRunRole(role, subject string) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return nil
	}
	if role != SourceRunRoleUI {
		return fmt.Errorf("%s.role %q is not supported (allowed: %q)", subject, role, SourceRunRoleUI)
	}
	return nil
}

func parsePhaseCommandFromJSON(data []byte, allowed map[string]struct{}, subject string) (SourcePhaseCommand, error) {
	if err := validateJSONWireObjectFields(data, allowed); err != nil {
		return SourcePhaseCommand{}, err
	}
	var raw sourcePhaseCommandWire
	if err := decodeJSONKnownFields(data, &raw); err != nil {
		return SourcePhaseCommand{}, err
	}
	if len(raw.Command) == 0 {
		return SourcePhaseCommand{}, fmt.Errorf("%s.command is required", subject)
	}
	if err := validateSourceRunRole(raw.Role, subject); err != nil {
		return SourcePhaseCommand{}, err
	}
	return SourcePhaseCommand(raw), nil
}

func parsePhaseCommandFromYAML(node *yaml.Node, allowed map[string]struct{}, subject string) (SourcePhaseCommand, error) {
	if node.Kind != yaml.MappingNode {
		return SourcePhaseCommand{}, fmt.Errorf("%s must be a mapping", subject)
	}
	if err := validateYAMLWireObjectFields(node, allowed, subject); err != nil {
		return SourcePhaseCommand{}, err
	}
	var raw sourcePhaseCommandWire
	if err := decodeYAMLKnownFields(node, &raw); err != nil {
		return SourcePhaseCommand{}, err
	}
	if len(raw.Command) == 0 {
		return SourcePhaseCommand{}, fmt.Errorf("%s.command is required", subject)
	}
	if err := validateSourceRunRole(raw.Role, subject); err != nil {
		return SourcePhaseCommand{}, err
	}
	return SourcePhaseCommand(raw), nil
}

func parsePhaseCommandListFromJSON(data []byte, allowed map[string]struct{}, subject string, scalarSeqOK bool) ([]SourcePhaseCommand, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("%s must be a sequence", subject)
	}
	var nodes []json.RawMessage
	if err := json.Unmarshal(trimmed, &nodes); err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s must not be empty", subject)
	}
	if scalarSeqOK && allJSONScalars(nodes) {
		var command []string
		if err := json.Unmarshal(trimmed, &command); err != nil {
			return nil, err
		}
		return []SourcePhaseCommand{{Command: command}}, nil
	}
	commands := make([]SourcePhaseCommand, 0, len(nodes))
	for i, node := range nodes {
		entrySubject := fmt.Sprintf("%s[%d]", subject, i)
		trimmedNode := bytes.TrimSpace(node)
		if len(trimmedNode) > 0 && trimmedNode[0] == '[' {
			var command []string
			if err := json.Unmarshal(trimmedNode, &command); err != nil {
				return nil, err
			}
			if len(command) == 0 {
				return nil, fmt.Errorf("%s.command is required", entrySubject)
			}
			commands = append(commands, SourcePhaseCommand{Command: command})
			continue
		}
		cmd, err := parsePhaseCommandFromJSON(trimmedNode, allowed, entrySubject)
		if err != nil {
			return nil, err
		}
		commands = append(commands, cmd)
	}
	return commands, nil
}

func parsePhaseCommandListFromYAML(node *yaml.Node, allowed map[string]struct{}, subject string, scalarSeqOK bool) ([]SourcePhaseCommand, error) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s must be a sequence", subject)
	}
	if len(node.Content) == 0 {
		return nil, fmt.Errorf("%s must not be empty", subject)
	}
	if scalarSeqOK && allYAMLScalars(node) {
		var command []string
		if err := node.Decode(&command); err != nil {
			return nil, err
		}
		return []SourcePhaseCommand{{Command: command}}, nil
	}
	commands := make([]SourcePhaseCommand, 0, len(node.Content))
	for i, child := range node.Content {
		entrySubject := fmt.Sprintf("%s[%d]", subject, i)
		switch child.Kind {
		case yaml.SequenceNode:
			var command []string
			if err := child.Decode(&command); err != nil {
				return nil, err
			}
			if len(command) == 0 {
				return nil, fmt.Errorf("%s.command is required", entrySubject)
			}
			commands = append(commands, SourcePhaseCommand{Command: command})
		case yaml.MappingNode:
			cmd, err := parsePhaseCommandFromYAML(child, allowed, entrySubject)
			if err != nil {
				return nil, err
			}
			commands = append(commands, cmd)
		default:
			return nil, fmt.Errorf("%s must be a sequence or mapping", entrySubject)
		}
	}
	return commands, nil
}

func allJSONScalars(nodes []json.RawMessage) bool {
	for _, node := range nodes {
		trimmed := bytes.TrimSpace(node)
		if len(trimmed) == 0 {
			return false
		}
		switch trimmed[0] {
		case '"', 'n', 't', 'f':
			continue
		default:
			return false
		}
	}
	return true
}

func allYAMLScalars(node *yaml.Node) bool {
	for _, child := range node.Content {
		if child.Kind != yaml.ScalarNode {
			return false
		}
	}
	return true
}

func syncLegacyPhaseFields(command *[]string, workdir *string, env *map[string]string, inputs *[]string, readyTimeout *string, commands []SourcePhaseCommand, wire phaseWireForm) {
	if len(commands) != 1 {
		return
	}
	if wire != phaseWireLegacyObject && wire != phaseWirePrepareOnlySequence {
		return
	}
	*command = append([]string(nil), commands[0].Command...)
	*workdir = commands[0].Workdir
	if commands[0].Env != nil {
		*env = commands[0].Env
	} else {
		*env = nil
	}
	*inputs = append([]string(nil), commands[0].Inputs...)
	*readyTimeout = commands[0].ReadyTimeout
}

func marshalPhaseCommandListJSONValue(commands []SourcePhaseCommand, allowed map[string]struct{}) []any {
	out := make([]any, 0, len(commands))
	for _, cmd := range commands {
		if cmd.Workdir == "" && len(cmd.Env) == 0 && len(cmd.Inputs) == 0 && cmd.ReadyTimeout == "" && cmd.Role == "" {
			out = append(out, append([]string(nil), cmd.Command...))
			continue
		}
		out = append(out, sourcePhaseCommandWire(cmd))
	}
	return out
}

func marshalPhaseCommandListJSON(commands []SourcePhaseCommand, allowed map[string]struct{}) ([]byte, error) {
	return json.Marshal(marshalPhaseCommandListJSONValue(commands, allowed))
}

func marshalPhaseCommandListYAML(commands []SourcePhaseCommand, allowed map[string]struct{}) ([]any, error) {
	out := make([]any, 0, len(commands))
	for _, cmd := range commands {
		if cmd.Workdir == "" && len(cmd.Env) == 0 && len(cmd.Inputs) == 0 && cmd.ReadyTimeout == "" && cmd.Role == "" {
			out = append(out, append([]string(nil), cmd.Command...))
			continue
		}
		out = append(out, sourcePhaseCommandWire(cmd))
	}
	return out, nil
}
