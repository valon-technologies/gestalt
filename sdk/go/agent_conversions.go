package gestalt

import (
	"fmt"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

func subjectFromProto(value *proto.SubjectContext) *Subject {
	if value == nil {
		return nil
	}
	return &Subject{
		ID:                  value.GetId(),
		CredentialSubjectID: value.GetCredentialSubjectId(),
		Email:               value.GetEmail(),
		DisplayName:         value.GetDisplayName(),
		Scopes:              cloneStrings(value.GetScopes()),
		Permissions:         subjectPermissionsFromProto(value.GetPermissions()),
	}
}

func subjectToProto(value *Subject) *proto.SubjectContext {
	if value == nil {
		return nil
	}
	return &proto.SubjectContext{
		Id:                  value.ID,
		CredentialSubjectId: value.CredentialSubjectID,
		Email:               value.Email,
		DisplayName:         value.DisplayName,
		Scopes:              cloneStrings(value.Scopes),
		Permissions:         subjectPermissionsToProto(value.Permissions),
	}
}

func agentToolRefFromProto(value *proto.AgentToolRef) AgentToolRef {
	if value == nil {
		return AgentToolRef{}
	}
	return AgentToolRef{
		App:            value.GetApp(),
		Operation:      value.GetOperation(),
		Connection:     value.GetConnection(),
		Instance:       value.GetInstance(),
		Title:          value.GetTitle(),
		Description:    value.GetDescription(),
		CredentialMode: value.GetCredentialMode(),
		System:         value.GetSystem(),
		RunAs:          subjectFromProto(value.GetRunAs()),
	}
}

func agentToolRefsFromProto(values []*proto.AgentToolRef) []AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]AgentToolRef, 0, len(values))
	for _, value := range values {
		out = append(out, agentToolRefFromProto(value))
	}
	return out
}

func agentToolRefToProto(value AgentToolRef) *proto.AgentToolRef {
	return &proto.AgentToolRef{
		App:            value.App,
		Operation:      value.Operation,
		Connection:     value.Connection,
		Instance:       value.Instance,
		Title:          value.Title,
		Description:    value.Description,
		CredentialMode: value.CredentialMode,
		System:         value.System,
		RunAs:          subjectToProto(value.RunAs),
	}
}

func agentToolRefsToProto(values []AgentToolRef) []*proto.AgentToolRef {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.AgentToolRef, 0, len(values))
	for _, value := range values {
		out = append(out, agentToolRefToProto(value))
	}
	return out
}

func agentOutputFromProto(value *proto.AgentOutput) *AgentOutput {
	if value == nil || value.GetKind() == nil {
		return nil
	}
	if value.GetText() != nil {
		return &AgentOutput{Text: &AgentTextOutput{}}
	}
	if structured := value.GetStructured(); structured != nil {
		return &AgentOutput{
			Structured: &AgentStructuredOutput{
				Schema: mapFromStruct(structured.GetSchema()),
			},
		}
	}
	return nil
}

func agentOutputToProto(value *AgentOutput) (*proto.AgentOutput, error) {
	if value == nil {
		return nil, nil
	}
	switch {
	case value.Text != nil && value.Structured != nil:
		return nil, fmt.Errorf("agent output cannot set both text and structured")
	case value.Text != nil:
		return &proto.AgentOutput{Kind: &proto.AgentOutput_Text{Text: &proto.AgentTextOutput{}}}, nil
	case value.Structured != nil:
		schema, err := structFromAny(value.Structured.Schema)
		if err != nil {
			return nil, err
		}
		return &proto.AgentOutput{
			Kind: &proto.AgentOutput_Structured{
				Structured: &proto.AgentStructuredOutput{Schema: schema},
			},
		}, nil
	default:
		return nil, nil
	}
}

func cloneAgentStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
