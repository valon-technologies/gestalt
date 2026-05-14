package models

import (
	"fmt"
	"strings"

	coremodel "github.com/valon-technologies/gestalt/server/core/model"
	proto "github.com/valon-technologies/gestalt/server/internal/gen/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func mapFromStruct(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func structFromMap(value map[string]any) (*structpb.Struct, error) {
	if value == nil {
		return nil, nil
	}
	return structpb.NewStruct(value)
}

func modelMessagesFromProto(values []*proto.ModelMessage) []coremodel.Message {
	if len(values) == 0 {
		return nil
	}
	out := make([]coremodel.Message, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		out = append(out, coremodel.Message{
			Role:     strings.TrimSpace(value.GetRole()),
			Text:     value.GetText(),
			Parts:    modelMessagePartsFromProto(value.GetParts()),
			Metadata: mapFromStruct(value.GetMetadata()),
		})
	}
	return out
}

func modelMessagePartsFromProto(values []*proto.ModelMessagePart) []coremodel.MessagePart {
	if len(values) == 0 {
		return nil
	}
	out := make([]coremodel.MessagePart, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		partType := coremodel.MessagePartTypeText
		if value.GetType() != proto.ModelMessagePartType_MODEL_MESSAGE_PART_TYPE_TEXT && value.GetType() != proto.ModelMessagePartType_MODEL_MESSAGE_PART_TYPE_UNSPECIFIED {
			partType = coremodel.MessagePartType(strings.TrimPrefix(value.GetType().String(), "MODEL_MESSAGE_PART_TYPE_"))
		}
		out = append(out, coremodel.MessagePart{
			Type: partType,
			Text: value.GetText(),
		})
	}
	return out
}

func modelMessagesToProto(values []coremodel.Message) ([]*proto.ModelMessage, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*proto.ModelMessage, 0, len(values))
	for _, value := range values {
		metadata, err := structFromMap(value.Metadata)
		if err != nil {
			return nil, fmt.Errorf("message metadata: %w", err)
		}
		out = append(out, &proto.ModelMessage{
			Role:     strings.TrimSpace(value.Role),
			Text:     value.Text,
			Parts:    modelMessagePartsToProto(value.Parts),
			Metadata: metadata,
		})
	}
	return out, nil
}

func modelMessagePartsToProto(values []coremodel.MessagePart) []*proto.ModelMessagePart {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.ModelMessagePart, 0, len(values))
	for _, value := range values {
		partType := proto.ModelMessagePartType_MODEL_MESSAGE_PART_TYPE_TEXT
		if value.Type != "" && value.Type != coremodel.MessagePartTypeText {
			partType = proto.ModelMessagePartType_MODEL_MESSAGE_PART_TYPE_UNSPECIFIED
		}
		out = append(out, &proto.ModelMessagePart{
			Type: partType,
			Text: value.Text,
		})
	}
	return out
}

func modelSubjectContextToProto(value coremodel.SubjectContext) *proto.ModelSubjectContext {
	if value == (coremodel.SubjectContext{}) {
		return nil
	}
	return &proto.ModelSubjectContext{
		SubjectId:           value.SubjectID,
		SubjectKind:         value.SubjectKind,
		CredentialSubjectId: value.CredentialSubjectID,
		DisplayName:         value.DisplayName,
		AuthSource:          value.AuthSource,
	}
}

func modelUsageToProto(value coremodel.Usage) *proto.ModelUsage {
	if value == (coremodel.Usage{}) {
		return nil
	}
	return &proto.ModelUsage{
		InputTokens:  value.InputTokens,
		OutputTokens: value.OutputTokens,
		TotalTokens:  value.TotalTokens,
	}
}

func modelUsageFromProto(value *proto.ModelUsage) coremodel.Usage {
	if value == nil {
		return coremodel.Usage{}
	}
	return coremodel.Usage{
		InputTokens:  value.GetInputTokens(),
		OutputTokens: value.GetOutputTokens(),
		TotalTokens:  value.GetTotalTokens(),
	}
}

func modelGenerateResponseToProto(value *coremodel.GenerateResponse) (*proto.GenerateModelResponse, error) {
	if value == nil {
		return nil, nil
	}
	structuredOutput, err := structFromMap(value.StructuredOutput)
	if err != nil {
		return nil, fmt.Errorf("structured output: %w", err)
	}
	providerMetadata, err := structFromMap(value.ProviderMetadata)
	if err != nil {
		return nil, fmt.Errorf("provider metadata: %w", err)
	}
	out := &proto.GenerateModelResponse{
		OutputText:       value.OutputText,
		StructuredOutput: structuredOutput,
		FinishReason:     value.FinishReason,
		Usage:            modelUsageToProto(value.Usage),
		ProviderMetadata: providerMetadata,
	}
	if value.Message.Role != "" || value.Message.Text != "" || len(value.Message.Parts) > 0 || len(value.Message.Metadata) > 0 {
		messageMetadata, err := structFromMap(value.Message.Metadata)
		if err != nil {
			return nil, fmt.Errorf("message metadata: %w", err)
		}
		out.Message = &proto.ModelMessage{
			Role:     value.Message.Role,
			Text:     value.Message.Text,
			Parts:    modelMessagePartsToProto(value.Message.Parts),
			Metadata: messageMetadata,
		}
	}
	return out, nil
}

func modelGenerateResponseFromProto(value *proto.GenerateModelResponse) (*coremodel.GenerateResponse, error) {
	if value == nil {
		return nil, nil
	}
	messages := modelMessagesFromProto([]*proto.ModelMessage{value.GetMessage()})
	var message coremodel.Message
	if len(messages) > 0 {
		message = messages[0]
	}
	return &coremodel.GenerateResponse{
		Message:          message,
		OutputText:       value.GetOutputText(),
		StructuredOutput: mapFromStruct(value.GetStructuredOutput()),
		FinishReason:     strings.TrimSpace(value.GetFinishReason()),
		Usage:            modelUsageFromProto(value.GetUsage()),
		ProviderMetadata: mapFromStruct(value.GetProviderMetadata()),
	}, nil
}

func modelProviderCapabilitiesFromProto(value *proto.ModelProviderCapabilities) *coremodel.ProviderCapabilities {
	if value == nil {
		return &coremodel.ProviderCapabilities{}
	}
	return &coremodel.ProviderCapabilities{
		TextOutput:       value.GetTextOutput(),
		StructuredOutput: value.GetStructuredOutput(),
		Usage:            value.GetUsage(),
		ParallelRequests: value.GetParallelRequests(),
	}
}
