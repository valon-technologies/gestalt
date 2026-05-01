package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/plugininvoker"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type proxyProvider struct {
	inner core.Provider
}

type invokePluginInput struct {
	Plugin          string         `json:"plugin"`
	Operation       string         `json:"operation"`
	Connection      string         `json:"connection,omitempty"`
	Instance        string         `json:"instance,omitempty"`
	InvocationToken string         `json:"invocation_token,omitempty"`
	Params          map[string]any `json:"params,omitempty"`
}

type invokePluginGraphQLInput struct {
	Plugin          string         `json:"plugin"`
	Document        string         `json:"document"`
	Connection      string         `json:"connection,omitempty"`
	Instance        string         `json:"instance,omitempty"`
	InvocationToken string         `json:"invocation_token,omitempty"`
	Variables       map[string]any `json:"variables,omitempty"`
}

type workflowScheduleTargetInput struct {
	Plugin     string         `json:"plugin"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type createWorkflowScheduleInput struct {
	ProviderName    string                      `json:"provider_name,omitempty"`
	Cron            string                      `json:"cron"`
	Timezone        string                      `json:"timezone,omitempty"`
	Target          workflowScheduleTargetInput `json:"target"`
	Paused          bool                        `json:"paused,omitempty"`
	InvocationToken string                      `json:"invocation_token,omitempty"`
}

type getWorkflowScheduleInput struct {
	ScheduleID      string `json:"schedule_id"`
	InvocationToken string `json:"invocation_token,omitempty"`
}

type updateWorkflowScheduleInput struct {
	ScheduleID      string                      `json:"schedule_id"`
	ProviderName    string                      `json:"provider_name,omitempty"`
	Cron            string                      `json:"cron"`
	Timezone        string                      `json:"timezone,omitempty"`
	Target          workflowScheduleTargetInput `json:"target"`
	Paused          bool                        `json:"paused,omitempty"`
	InvocationToken string                      `json:"invocation_token,omitempty"`
}

type workflowEventMatchInput struct {
	Type    string `json:"type"`
	Source  string `json:"source,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type createWorkflowTriggerInput struct {
	ProviderName    string                      `json:"provider_name,omitempty"`
	Match           workflowEventMatchInput     `json:"match"`
	Target          workflowScheduleTargetInput `json:"target"`
	Paused          bool                        `json:"paused,omitempty"`
	InvocationToken string                      `json:"invocation_token,omitempty"`
}

type getWorkflowTriggerInput struct {
	TriggerID       string `json:"trigger_id"`
	InvocationToken string `json:"invocation_token,omitempty"`
}

type updateWorkflowTriggerInput struct {
	TriggerID       string                      `json:"trigger_id"`
	ProviderName    string                      `json:"provider_name,omitempty"`
	Match           workflowEventMatchInput     `json:"match"`
	Target          workflowScheduleTargetInput `json:"target"`
	Paused          bool                        `json:"paused,omitempty"`
	InvocationToken string                      `json:"invocation_token,omitempty"`
}

type publishWorkflowEventInput struct {
	ID              string         `json:"id,omitempty"`
	Source          string         `json:"source,omitempty"`
	SpecVersion     string         `json:"spec_version,omitempty"`
	Type            string         `json:"type"`
	Subject         string         `json:"subject,omitempty"`
	Time            string         `json:"time,omitempty"`
	DataContentType string         `json:"data_content_type,omitempty"`
	Data            map[string]any `json:"data,omitempty"`
	Extensions      map[string]any `json:"extensions,omitempty"`
	InvocationToken string         `json:"invocation_token,omitempty"`
}

func newProxyProvider(inner core.Provider) *proxyProvider {
	return &proxyProvider{inner: inner}
}

func (p *proxyProvider) Name() string                        { return p.inner.Name() }
func (p *proxyProvider) DisplayName() string                 { return p.inner.DisplayName() }
func (p *proxyProvider) Description() string                 { return p.inner.Description() }
func (p *proxyProvider) ConnectionMode() core.ConnectionMode { return p.inner.ConnectionMode() }
func (p *proxyProvider) AuthTypes() []string                 { return p.inner.AuthTypes() }
func (p *proxyProvider) ConnectionParamDefs() map[string]core.ConnectionParamDef {
	return p.inner.ConnectionParamDefs()
}
func (p *proxyProvider) CredentialFields() []core.CredentialFieldDef {
	return p.inner.CredentialFields()
}
func (p *proxyProvider) DiscoveryConfig() *core.DiscoveryConfig {
	return p.inner.DiscoveryConfig()
}
func (p *proxyProvider) ConnectionForOperation(operation string) string {
	return p.inner.ConnectionForOperation(operation)
}
func (p *proxyProvider) Catalog() *catalog.Catalog {
	var cat *catalog.Catalog
	if inner := p.inner.Catalog(); inner != nil {
		cat = inner.Clone()
	} else {
		cat = &catalog.Catalog{
			Name:        p.Name(),
			DisplayName: p.DisplayName(),
			Description: p.Description(),
		}
	}
	cat.Operations = append(cat.Operations,
		catalog.CatalogOperation{
			ID:        "read_env",
			Method:    http.MethodGet,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "name", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "read_file",
			Method:    http.MethodGet,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "path", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "make_http_request",
			Method:    http.MethodGet,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "url", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "invoke_plugin",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "plugin", Type: "string", Description: "Target plugin name", Required: true},
				{Name: "operation", Type: "string", Description: "Target operation id", Required: true},
				{Name: "connection", Type: "string", Description: "Optional connection override"},
				{Name: "instance", Type: "string", Description: "Optional target instance override"},
				{Name: "invocation_token", Type: "string", Description: "Optional invocation token override for token propagation tests"},
				{Name: "params", Type: "object", Description: "Nested params forwarded to the target operation"},
			},
		},
		catalog.CatalogOperation{
			ID:        "invoke_plugin_graphql",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "plugin", Type: "string", Description: "Target plugin name", Required: true},
				{Name: "document", Type: "string", Description: "GraphQL document forwarded to the target plugin", Required: true},
				{Name: "connection", Type: "string", Description: "Optional connection override"},
				{Name: "instance", Type: "string", Description: "Optional target instance override"},
				{Name: "invocation_token", Type: "string", Description: "Optional invocation token override for token propagation tests"},
				{Name: "variables", Type: "object", Description: "Variables forwarded to the target GraphQL surface"},
			},
		},
		catalog.CatalogOperation{ID: "create_workflow_schedule", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "get_workflow_schedule", Method: http.MethodGet, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "update_workflow_schedule", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "delete_workflow_schedule", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "pause_workflow_schedule", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "resume_workflow_schedule", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "create_workflow_trigger", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "get_workflow_trigger", Method: http.MethodGet, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "update_workflow_trigger", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "delete_workflow_trigger", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "pause_workflow_trigger", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "resume_workflow_trigger", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{ID: "publish_workflow_event", Method: http.MethodPost, Transport: catalog.TransportPlugin},
		catalog.CatalogOperation{
			ID:        "indexeddb_roundtrip",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "binding", Type: "string"},
				{Name: "store", Type: "string", Required: true},
				{Name: "id", Type: "string", Required: true},
				{Name: "value", Type: "string", Required: true},
			},
		},
		catalog.CatalogOperation{
			ID:        "s3_roundtrip",
			Method:    http.MethodPost,
			Transport: catalog.TransportPlugin,
			Parameters: []catalog.CatalogParameter{
				{Name: "binding", Type: "string"},
				{Name: "bucket", Type: "string", Required: true},
				{Name: "key", Type: "string", Required: true},
				{Name: "value", Type: "string", Required: true},
			},
		},
	)
	return cat
}

func (p *proxyProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*core.OperationResult, error) {
	switch operation {
	case "invoke_plugin":
		input, err := decodeInvokePluginInput(params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		if strings.TrimSpace(input.Plugin) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "plugin is required"}), nil
		}
		if strings.TrimSpace(input.Operation) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "operation is required"}), nil
		}

		envelope := map[string]any{
			"ok":                       false,
			"target_plugin":            input.Plugin,
			"target_operation":         input.Operation,
			"used_connection_override": strings.TrimSpace(input.Connection) != "",
		}

		invocationToken := input.InvocationToken
		if invocationToken == "" {
			invocationToken = plugininvoker.InvocationTokenFromContext(ctx)
		}
		if invocationToken == "" {
			envelope["error"] = "invocation token is not available"
			return jsonResult(http.StatusOK, envelope), nil
		}

		invoker, err := gestalt.Invoker(invocationToken)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}
		defer func() { _ = invoker.Close() }()

		connection := strings.TrimSpace(input.Connection)
		instance := strings.TrimSpace(input.Instance)
		var opts *gestalt.InvokeOptions
		if connection != "" || instance != "" {
			opts = &gestalt.InvokeOptions{
				Connection: connection,
				Instance:   instance,
			}
		}
		result, err := invoker.Invoke(ctx, input.Plugin, input.Operation, input.Params, opts)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}
		envelope["ok"] = true
		envelope["status"] = result.Status
		envelope["body"] = decodeResultBody(result.Body)
		return jsonResult(http.StatusOK, envelope), nil

	case "invoke_plugin_graphql":
		input, err := decodeInvokePluginGraphQLInput(params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		if strings.TrimSpace(input.Plugin) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "plugin is required"}), nil
		}
		if strings.TrimSpace(input.Document) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "document is required"}), nil
		}

		envelope := map[string]any{
			"ok":                       false,
			"target_plugin":            input.Plugin,
			"target_operation":         "graphql",
			"used_connection_override": strings.TrimSpace(input.Connection) != "",
		}

		invocationToken := input.InvocationToken
		if invocationToken == "" {
			invocationToken = plugininvoker.InvocationTokenFromContext(ctx)
		}
		if invocationToken == "" {
			envelope["error"] = "invocation token is not available"
			return jsonResult(http.StatusOK, envelope), nil
		}

		invoker, err := gestalt.Invoker(invocationToken)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}
		defer func() { _ = invoker.Close() }()

		connection := strings.TrimSpace(input.Connection)
		instance := strings.TrimSpace(input.Instance)
		var opts *gestalt.InvokeOptions
		if connection != "" || instance != "" {
			opts = &gestalt.InvokeOptions{
				Connection: connection,
				Instance:   instance,
			}
		}
		result, err := invoker.InvokeGraphQL(ctx, input.Plugin, input.Document, input.Variables, opts)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}
		envelope["ok"] = true
		envelope["status"] = result.Status
		envelope["body"] = decodeResultBody(result.Body)
		return jsonResult(http.StatusOK, envelope), nil

	case "create_workflow_schedule":
		input, err := decodeJSONParams[createWorkflowScheduleInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInputToProto(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.CreateSchedule(ctx, &proto.WorkflowManagerCreateScheduleRequest{
			ProviderName: input.ProviderName,
			Cron:         input.Cron,
			Timezone:     input.Timezone,
			Target:       target,
			Paused:       input.Paused,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowScheduleBody(result)), nil

	case "get_workflow_schedule":
		input, err := decodeJSONParams[getWorkflowScheduleInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.GetSchedule(ctx, &proto.WorkflowManagerGetScheduleRequest{
			ScheduleId: input.ScheduleID,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowScheduleBody(result)), nil

	case "update_workflow_schedule":
		input, err := decodeJSONParams[updateWorkflowScheduleInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInputToProto(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpdateSchedule(ctx, &proto.WorkflowManagerUpdateScheduleRequest{
			ScheduleId:   input.ScheduleID,
			ProviderName: input.ProviderName,
			Cron:         input.Cron,
			Timezone:     input.Timezone,
			Target:       target,
			Paused:       input.Paused,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowScheduleBody(result)), nil

	case "delete_workflow_schedule":
		input, err := decodeJSONParams[getWorkflowScheduleInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		if err := client.DeleteSchedule(ctx, &proto.WorkflowManagerDeleteScheduleRequest{
			ScheduleId: input.ScheduleID,
		}); err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, map[string]any{"deleted": true, "schedule_id": input.ScheduleID}), nil

	case "pause_workflow_schedule":
		input, err := decodeJSONParams[getWorkflowScheduleInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.PauseSchedule(ctx, &proto.WorkflowManagerPauseScheduleRequest{
			ScheduleId: input.ScheduleID,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowScheduleBody(result)), nil

	case "resume_workflow_schedule":
		input, err := decodeJSONParams[getWorkflowScheduleInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.ResumeSchedule(ctx, &proto.WorkflowManagerResumeScheduleRequest{
			ScheduleId: input.ScheduleID,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowScheduleBody(result)), nil

	case "create_workflow_trigger":
		input, err := decodeJSONParams[createWorkflowTriggerInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInputToProto(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.CreateTrigger(ctx, &proto.WorkflowManagerCreateEventTriggerRequest{
			ProviderName: input.ProviderName,
			Match:        workflowEventMatchInputToProto(input.Match),
			Target:       target,
			Paused:       input.Paused,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowTriggerBody(result)), nil

	case "get_workflow_trigger":
		input, err := decodeJSONParams[getWorkflowTriggerInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.GetTrigger(ctx, &proto.WorkflowManagerGetEventTriggerRequest{
			TriggerId: input.TriggerID,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowTriggerBody(result)), nil

	case "update_workflow_trigger":
		input, err := decodeJSONParams[updateWorkflowTriggerInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInputToProto(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpdateTrigger(ctx, &proto.WorkflowManagerUpdateEventTriggerRequest{
			TriggerId:    input.TriggerID,
			ProviderName: input.ProviderName,
			Match:        workflowEventMatchInputToProto(input.Match),
			Target:       target,
			Paused:       input.Paused,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowTriggerBody(result)), nil

	case "delete_workflow_trigger":
		input, err := decodeJSONParams[getWorkflowTriggerInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		if err := client.DeleteTrigger(ctx, &proto.WorkflowManagerDeleteEventTriggerRequest{
			TriggerId: input.TriggerID,
		}); err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, map[string]any{"deleted": true, "trigger_id": input.TriggerID}), nil

	case "pause_workflow_trigger":
		input, err := decodeJSONParams[getWorkflowTriggerInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.PauseTrigger(ctx, &proto.WorkflowManagerPauseEventTriggerRequest{
			TriggerId: input.TriggerID,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowTriggerBody(result)), nil

	case "resume_workflow_trigger":
		input, err := decodeJSONParams[getWorkflowTriggerInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.ResumeTrigger(ctx, &proto.WorkflowManagerResumeEventTriggerRequest{
			TriggerId: input.TriggerID,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, managedWorkflowTriggerBody(result)), nil

	case "publish_workflow_event":
		input, err := decodeJSONParams[publishWorkflowEventInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		event, err := workflowEventInputToProto(input)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.PublishEvent(ctx, &proto.WorkflowManagerPublishEventRequest{
			Event: event,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, workflowEventBody(result)), nil

	case "read_env":
		name, _ := params["name"].(string)
		val, ok := os.LookupEnv(name)
		body, _ := json.Marshal(map[string]any{"name": name, "value": val, "found": ok})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "read_file":
		path, _ := params["path"].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsPermission(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &core.OperationResult{Status: http.StatusForbidden, Body: string(body)}, nil
			}
			if os.IsNotExist(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &core.OperationResult{Status: http.StatusNotFound, Body: string(body)}, nil
			}
			body, _ := json.Marshal(map[string]any{"error": err.Error()})
			return &core.OperationResult{Status: http.StatusInternalServerError, Body: string(body)}, nil
		}
		body, _ := json.Marshal(map[string]any{"content": string(data)})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "make_http_request":
		targetURL, _ := params["url"].(string)
		client := &http.Client{}
		if proxyURL := os.Getenv("HTTP_PROXY"); proxyURL != "" {
			parsed, err := url.Parse(proxyURL)
			if err == nil {
				client.Transport = &http.Transport{
					Proxy:           http.ProxyURL(parsed),
					TLSClientConfig: testTLSConfigFromEnv(),
				}
			}
		}
		resp, err := client.Get(targetURL)
		if err != nil {
			body, _ := json.Marshal(map[string]any{"error": err.Error()})
			return &core.OperationResult{Status: http.StatusBadGateway, Body: string(body)}, nil
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		body, _ := json.Marshal(map[string]any{
			"status": resp.StatusCode,
			"body":   string(respBody),
		})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "indexeddb_roundtrip":
		binding, _ := params["binding"].(string)
		store, _ := params["store"].(string)
		id, _ := params["id"].(string)
		value, _ := params["value"].(string)

		var (
			db  *gestalt.IndexedDBClient
			err error
		)
		if binding != "" {
			db, err = gestalt.IndexedDB(binding)
		} else {
			db, err = gestalt.IndexedDB()
		}
		if err != nil {
			return nil, err
		}
		defer func() { _ = db.Close() }()

		if err := db.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{}); err != nil && !errors.Is(err, gestalt.ErrAlreadyExists) {
			return nil, err
		}
		if err := db.ObjectStore(store).Put(ctx, map[string]any{"id": id, "value": value}); err != nil {
			return nil, err
		}
		record, err := db.ObjectStore(store).Get(ctx, id)
		if err != nil {
			return nil, err
		}
		body, _ := json.Marshal(record)
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "s3_roundtrip":
		binding, _ := params["binding"].(string)
		bucket, _ := params["bucket"].(string)
		key, _ := params["key"].(string)
		value, _ := params["value"].(string)

		var (
			client *gestalt.S3Client
			err    error
		)
		if binding != "" {
			client, err = gestalt.S3(binding)
		} else {
			client, err = gestalt.S3()
		}
		if err != nil {
			return nil, err
		}
		defer func() { _ = client.Close() }()

		obj := client.Object(bucket, key)
		if _, err := obj.WriteString(ctx, value, &gestalt.WriteOptions{ContentType: "text/plain"}); err != nil {
			return nil, err
		}
		text, err := obj.Text(ctx, nil)
		if err != nil {
			return nil, err
		}
		stat, err := obj.Stat(ctx)
		if err != nil {
			return nil, err
		}
		page, err := client.ListObjects(ctx, gestalt.ListOptions{Bucket: bucket, Prefix: key})
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(page.Objects))
		for i := range page.Objects {
			keys = append(keys, page.Objects[i].Ref.Key)
		}
		body, _ := json.Marshal(map[string]any{
			"body":  text,
			"key":   stat.Ref.Key,
			"size":  stat.Size,
			"keys":  keys,
			"type":  stat.ContentType,
			"etag":  stat.ETag,
			"found": len(page.Objects) > 0,
		})
		return &core.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	default:
		return p.inner.Execute(ctx, operation, params, token)
	}
}

func (p *proxyProvider) Close() error {
	return nil
}

func decodeInvokePluginInput(params map[string]any) (invokePluginInput, error) {
	return decodeJSONParams[invokePluginInput](params)
}

func decodeInvokePluginGraphQLInput(params map[string]any) (invokePluginGraphQLInput, error) {
	return decodeJSONParams[invokePluginGraphQLInput](params)
}

func decodeJSONParams[T any](params map[string]any) (T, error) {
	if params == nil {
		params = map[string]any{}
	}
	var input T
	data, err := json.Marshal(params)
	if err != nil {
		return input, err
	}
	if err := json.Unmarshal(data, &input); err != nil {
		return input, err
	}
	return input, nil
}

func workflowManagerFromContext(ctx context.Context, invocationToken string) (*gestalt.WorkflowManagerClient, error) {
	token := strings.TrimSpace(invocationToken)
	if token == "" {
		token = plugininvoker.InvocationTokenFromContext(ctx)
	}
	return gestalt.WorkflowManager(token)
}

func workflowTargetInputToProto(target workflowScheduleTargetInput) (*proto.BoundWorkflowTarget, error) {
	input, err := structpb.NewStruct(target.Input)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowTarget{
		Kind: &proto.BoundWorkflowTarget_Plugin{
			Plugin: &proto.BoundWorkflowPluginTarget{
				PluginName: target.Plugin,
				Operation:  target.Operation,
				Connection: target.Connection,
				Instance:   target.Instance,
				Input:      input,
			},
		},
	}, nil
}

func workflowEventMatchInputToProto(match workflowEventMatchInput) *proto.WorkflowEventMatch {
	return &proto.WorkflowEventMatch{
		Type:    match.Type,
		Source:  match.Source,
		Subject: match.Subject,
	}
}

func workflowEventInputToProto(input publishWorkflowEventInput) (*proto.WorkflowEvent, error) {
	data, err := structpb.NewStruct(input.Data)
	if err != nil {
		return nil, err
	}
	extensions, err := structpb.NewStruct(input.Extensions)
	if err != nil {
		return nil, err
	}
	event := &proto.WorkflowEvent{
		Id:              input.ID,
		Source:          input.Source,
		SpecVersion:     input.SpecVersion,
		Type:            input.Type,
		Subject:         input.Subject,
		Datacontenttype: input.DataContentType,
		Data:            data,
		Extensions:      extensions.GetFields(),
	}
	if strings.TrimSpace(input.Time) != "" {
		timestamp, err := time.Parse(time.RFC3339, input.Time)
		if err != nil {
			return nil, err
		}
		event.Time = timestamppb.New(timestamp)
	}
	return event, nil
}

func managedWorkflowScheduleBody(value *proto.ManagedWorkflowSchedule) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	schedule := value.GetSchedule()
	body := map[string]any{
		"provider_name": value.GetProviderName(),
	}
	if schedule == nil {
		return body
	}
	target := schedule.GetTarget()
	body["schedule"] = map[string]any{
		"id":         schedule.GetId(),
		"cron":       schedule.GetCron(),
		"timezone":   schedule.GetTimezone(),
		"paused":     schedule.GetPaused(),
		"created_at": timestampBody(schedule.GetCreatedAt()),
		"updated_at": timestampBody(schedule.GetUpdatedAt()),
		"next_run_at": func() any {
			if schedule.GetNextRunAt() == nil {
				return nil
			}
			return schedule.GetNextRunAt().AsTime()
		}(),
		"target": map[string]any{
			"plugin":     "",
			"operation":  "",
			"connection": "",
			"instance":   "",
			"input":      map[string]any{},
		},
	}
	if target != nil {
		pluginTarget := target.GetPlugin()
		body["schedule"].(map[string]any)["target"] = map[string]any{
			"plugin":     pluginTarget.GetPluginName(),
			"operation":  pluginTarget.GetOperation(),
			"connection": pluginTarget.GetConnection(),
			"instance":   pluginTarget.GetInstance(),
			"input":      workflowPluginTargetInputMap(pluginTarget),
		}
	}
	return body
}

func managedWorkflowTriggerBody(value *proto.ManagedWorkflowEventTrigger) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	trigger := value.GetTrigger()
	body := map[string]any{
		"provider_name": value.GetProviderName(),
	}
	if trigger == nil {
		return body
	}
	target := trigger.GetTarget()
	match := trigger.GetMatch()
	body["trigger"] = map[string]any{
		"id":         trigger.GetId(),
		"paused":     trigger.GetPaused(),
		"created_at": timestampBody(trigger.GetCreatedAt()),
		"updated_at": timestampBody(trigger.GetUpdatedAt()),
		"match": map[string]any{
			"type":    "",
			"source":  "",
			"subject": "",
		},
		"target": map[string]any{
			"plugin":     "",
			"operation":  "",
			"connection": "",
			"instance":   "",
			"input":      map[string]any{},
		},
	}
	if match != nil {
		body["trigger"].(map[string]any)["match"] = map[string]any{
			"type":    match.GetType(),
			"source":  match.GetSource(),
			"subject": match.GetSubject(),
		}
	}
	if target != nil {
		pluginTarget := target.GetPlugin()
		body["trigger"].(map[string]any)["target"] = map[string]any{
			"plugin":     pluginTarget.GetPluginName(),
			"operation":  pluginTarget.GetOperation(),
			"connection": pluginTarget.GetConnection(),
			"instance":   pluginTarget.GetInstance(),
			"input":      workflowPluginTargetInputMap(pluginTarget),
		}
	}
	return body
}

func workflowPluginTargetInputMap(target *proto.BoundWorkflowPluginTarget) map[string]any {
	if target == nil || target.GetInput() == nil {
		return map[string]any{}
	}
	return target.GetInput().AsMap()
}

func workflowEventBody(value *proto.WorkflowEvent) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                value.GetId(),
		"source":            value.GetSource(),
		"spec_version":      value.GetSpecVersion(),
		"type":              value.GetType(),
		"subject":           value.GetSubject(),
		"time":              timestampBody(value.GetTime()),
		"data_content_type": value.GetDatacontenttype(),
		"data":              value.GetData().AsMap(),
		"extensions": func() map[string]any {
			if len(value.GetExtensions()) == 0 {
				return map[string]any{}
			}
			out := make(map[string]any, len(value.GetExtensions()))
			for key, field := range value.GetExtensions() {
				if field != nil {
					out[key] = field.AsInterface()
				}
			}
			return out
		}(),
	}
}

func timestampBody(value *timestamppb.Timestamp) any {
	if value == nil {
		return nil
	}
	return value.AsTime()
}

func decodeResultBody(body string) any {
	var decoded any
	if err := json.Unmarshal([]byte(body), &decoded); err == nil {
		return decoded
	}
	return body
}

func testTLSConfigFromEnv() *tls.Config {
	pemBytes := []byte(strings.TrimSpace(os.Getenv(gestalt.EnvHostServiceTLSCAPEM)))
	caFile := strings.TrimSpace(os.Getenv(gestalt.EnvHostServiceTLSCAFile))
	if len(pemBytes) == 0 && caFile == "" {
		return nil
	}
	if len(pemBytes) == 0 {
		var err error
		pemBytes, err = os.ReadFile(caFile)
		if err != nil {
			return nil
		}
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
}

func jsonResult(status int, body any) *core.OperationResult {
	data, _ := json.Marshal(body)
	return &core.OperationResult{Status: status, Body: string(data)}
}
