package model

import (
	"context"
	"fmt"
)

type Message struct {
	Role     string
	Text     string
	Parts    []MessagePart
	Metadata map[string]any
}

type MessagePartType string

const (
	MessagePartTypeText MessagePartType = "text"
)

type MessagePart struct {
	Type MessagePartType
	Text string
}

type SubjectContext struct {
	SubjectID           string
	SubjectKind         string
	CredentialSubjectID string
	DisplayName         string
	AuthSource          string
}

type Usage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

type ProviderCapabilities struct {
	TextOutput       bool
	StructuredOutput bool
	Usage            bool
	ParallelRequests bool
}

type GetCapabilitiesRequest struct{}

type GenerateRequest struct {
	ProviderName     string
	Model            string
	Messages         []Message
	ResponseSchema   map[string]any
	ModelOptions     map[string]any
	Metadata         map[string]any
	Subject          SubjectContext
	CallerPluginName string
}

type GenerateResponse struct {
	Message          Message
	OutputText       string
	StructuredOutput map[string]any
	FinishReason     string
	Usage            Usage
	ProviderMetadata map[string]any
}

type ManagerGenerateRequest struct {
	ProviderName     string
	Model            string
	Messages         []Message
	ResponseSchema   map[string]any
	ModelOptions     map[string]any
	Metadata         map[string]any
	CallerPluginName string
}

type Provider interface {
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
	GetCapabilities(ctx context.Context, req GetCapabilitiesRequest) (*ProviderCapabilities, error)
	Ping(ctx context.Context) error
	Close() error
}

type UnimplementedProvider struct{}

func (UnimplementedProvider) Generate(context.Context, GenerateRequest) (*GenerateResponse, error) {
	return nil, fmt.Errorf("model provider generate is not implemented")
}

func (UnimplementedProvider) GetCapabilities(context.Context, GetCapabilitiesRequest) (*ProviderCapabilities, error) {
	return nil, fmt.Errorf("model provider get capabilities is not implemented")
}

func (UnimplementedProvider) Ping(context.Context) error {
	return fmt.Errorf("model provider ping is not implemented")
}

func (UnimplementedProvider) Close() error {
	return nil
}
