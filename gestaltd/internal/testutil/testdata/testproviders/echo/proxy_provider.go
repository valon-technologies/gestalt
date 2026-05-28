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
	s3sdk "github.com/valon-technologies/gestalt/sdk/go/s3"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type executableProvider interface {
	Configure(context.Context, string, map[string]any) error
	Execute(context.Context, string, map[string]any, string) (*gestalt.OperationResult, error)
}

type invocationTokenContextKey struct{}

type proxyProvider struct {
	inner executableProvider
}

func withInvocationToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, invocationTokenContextKey{}, strings.TrimSpace(token))
}

func invocationTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(invocationTokenContextKey{}).(string)
	return strings.TrimSpace(token)
}

type invokePluginInput struct {
	App             string         `json:"app"`
	Operation       string         `json:"operation"`
	Connection      string         `json:"connection,omitempty"`
	Instance        string         `json:"instance,omitempty"`
	InvocationToken string         `json:"invocation_token,omitempty"`
	Params          map[string]any `json:"params,omitempty"`
}

type invokePluginGraphQLInput struct {
	App             string         `json:"app"`
	Document        string         `json:"document"`
	Connection      string         `json:"connection,omitempty"`
	Instance        string         `json:"instance,omitempty"`
	InvocationToken string         `json:"invocation_token,omitempty"`
	Variables       map[string]any `json:"variables,omitempty"`
}

type workflowScheduleTargetInput struct {
	App        string         `json:"app"`
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
	ProviderName    string         `json:"provider_name,omitempty"`
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

type echoInput struct {
	Message string `json:"message,omitempty"`
	Echo    string `json:"echo,omitempty"`
}

type readEnvInput struct {
	Name string `json:"name" required:"true"`
}

type readFileInput struct {
	Path string `json:"path" required:"true"`
}

type makeHTTPRequestInput struct {
	URL string `json:"url" required:"true"`
}

type indexedDBRoundtripInput struct {
	Binding string `json:"binding,omitempty"`
	Store   string `json:"store" required:"true"`
	ID      string `json:"id" required:"true"`
	Value   string `json:"value" required:"true"`
}

type s3RoundtripInput struct {
	Binding string `json:"binding,omitempty"`
	Key     string `json:"key" required:"true"`
	Value   string `json:"value" required:"true"`
}

var proxyRouter = gestalt.MustRouter(
	proxyOperation[echoInput]("echo", http.MethodPost),
	proxyOperation[readEnvInput]("read_env", http.MethodGet),
	proxyOperation[readFileInput]("read_file", http.MethodGet),
	proxyOperation[makeHTTPRequestInput]("make_http_request", http.MethodGet),
	proxyOperation[invokePluginInput]("invoke_plugin", http.MethodPost),
	proxyOperation[invokePluginGraphQLInput]("invoke_plugin_graphql", http.MethodPost),
	proxyOperation[createWorkflowScheduleInput]("create_workflow_schedule", http.MethodPost),
	proxyOperation[getWorkflowScheduleInput]("get_workflow_schedule", http.MethodGet),
	proxyOperation[updateWorkflowScheduleInput]("update_workflow_schedule", http.MethodPost),
	proxyOperation[getWorkflowScheduleInput]("delete_workflow_schedule", http.MethodPost),
	proxyOperation[getWorkflowScheduleInput]("pause_workflow_schedule", http.MethodPost),
	proxyOperation[getWorkflowScheduleInput]("resume_workflow_schedule", http.MethodPost),
	proxyOperation[createWorkflowTriggerInput]("create_workflow_trigger", http.MethodPost),
	proxyOperation[getWorkflowTriggerInput]("get_workflow_trigger", http.MethodGet),
	proxyOperation[updateWorkflowTriggerInput]("update_workflow_trigger", http.MethodPost),
	proxyOperation[getWorkflowTriggerInput]("delete_workflow_trigger", http.MethodPost),
	proxyOperation[getWorkflowTriggerInput]("pause_workflow_trigger", http.MethodPost),
	proxyOperation[getWorkflowTriggerInput]("resume_workflow_trigger", http.MethodPost),
	proxyOperation[publishWorkflowEventInput]("publish_workflow_event", http.MethodPost),
	proxyOperation[indexedDBRoundtripInput]("indexeddb_roundtrip", http.MethodPost),
	proxyOperation[s3RoundtripInput]("s3_roundtrip", http.MethodPost),
)

func proxyOperation[In any](id, method string) gestalt.Registration[proxyProvider] {
	return gestalt.Register(
		gestalt.Operation[In, json.RawMessage]{ID: id, Method: method},
		func(p *proxyProvider, ctx context.Context, input In, req gestalt.Request) (gestalt.Response[json.RawMessage], error) {
			params, err := inputParams(input)
			if err != nil {
				return gestalt.Response[json.RawMessage]{}, err
			}
			result, err := p.Execute(withInvocationToken(ctx, req.InvocationToken()), id, params, req.Token)
			if err != nil {
				return gestalt.Response[json.RawMessage]{}, err
			}
			if result == nil {
				return gestalt.Response[json.RawMessage]{
					Status: http.StatusInternalServerError,
					Body:   json.RawMessage(`{"error":"nil operation result"}`),
				}, nil
			}
			body := json.RawMessage(result.Body)
			if len(body) == 0 {
				body = json.RawMessage(`null`)
			}
			return gestalt.Response[json.RawMessage]{Status: result.Status, Body: body}, nil
		},
	)
}

func inputParams(input any) (map[string]any, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	params := map[string]any{}
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, err
	}
	return params, nil
}

func newProxyProvider(inner executableProvider) *proxyProvider {
	return &proxyProvider{inner: inner}
}

func (p *proxyProvider) Configure(ctx context.Context, name string, config map[string]any) error {
	return p.inner.Configure(ctx, name, config)
}

func (p *proxyProvider) Execute(ctx context.Context, operation string, params map[string]any, token string) (*gestalt.OperationResult, error) {
	switch operation {
	case "invoke_plugin":
		input, err := decodeInvokePluginInput(params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		if strings.TrimSpace(input.App) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "app is required"}), nil
		}
		if strings.TrimSpace(input.Operation) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "operation is required"}), nil
		}

		envelope := map[string]any{
			"ok":                       false,
			"target_app":               input.App,
			"target_operation":         input.Operation,
			"used_connection_override": strings.TrimSpace(input.Connection) != "",
		}

		invocationToken := input.InvocationToken
		if invocationToken == "" {
			invocationToken = invocationTokenFromContext(ctx)
		}
		if invocationToken == "" {
			envelope["error"] = "invocation token is not available"
			return jsonResult(http.StatusOK, envelope), nil
		}

		invoker, err := gestalt.NewApp(invocationToken)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}

		connection := strings.TrimSpace(input.Connection)
		instance := strings.TrimSpace(input.Instance)
		var opts *gestalt.InvokeOptions
		if connection != "" || instance != "" {
			opts = &gestalt.InvokeOptions{
				Connection: connection,
				Instance:   instance,
			}
		}
		result, err := invoker.Invoke(ctx, input.App, input.Operation, input.Params, opts)
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
		if strings.TrimSpace(input.App) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "app is required"}), nil
		}
		if strings.TrimSpace(input.Document) == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "document is required"}), nil
		}

		envelope := map[string]any{
			"ok":                       false,
			"target_app":               input.App,
			"target_operation":         "graphql",
			"used_connection_override": strings.TrimSpace(input.Connection) != "",
		}

		invocationToken := input.InvocationToken
		if invocationToken == "" {
			invocationToken = invocationTokenFromContext(ctx)
		}
		if invocationToken == "" {
			envelope["error"] = "invocation token is not available"
			return jsonResult(http.StatusOK, envelope), nil
		}

		invoker, err := gestalt.NewApp(invocationToken)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}

		connection := strings.TrimSpace(input.Connection)
		instance := strings.TrimSpace(input.Instance)
		var opts *gestalt.InvokeGraphQLOptions
		if connection != "" || instance != "" {
			opts = &gestalt.InvokeGraphQLOptions{
				Connection: connection,
				Instance:   instance,
			}
		}
		result, err := invoker.InvokeGraphQL(ctx, input.App, input.Document, input.Variables, opts)
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpsertSchedule(ctx, &proto.UpsertWorkflowProviderScheduleRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.GetSchedule(ctx, &proto.GetWorkflowProviderScheduleRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpsertSchedule(ctx, &proto.UpsertWorkflowProviderScheduleRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		if _, err := client.DeleteSchedule(ctx, &proto.DeleteWorkflowProviderScheduleRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.PauseSchedule(ctx, &proto.PauseWorkflowProviderScheduleRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.ResumeSchedule(ctx, &proto.ResumeWorkflowProviderScheduleRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpsertEventTrigger(ctx, &proto.UpsertWorkflowProviderEventTriggerRequest{
			ProviderName: input.ProviderName,
			Match:        workflowEventMatch(input.Match),
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.GetEventTrigger(ctx, &proto.GetWorkflowProviderEventTriggerRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpsertEventTrigger(ctx, &proto.UpsertWorkflowProviderEventTriggerRequest{
			TriggerId:    input.TriggerID,
			ProviderName: input.ProviderName,
			Match:        workflowEventMatch(input.Match),
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		if _, err := client.DeleteEventTrigger(ctx, &proto.DeleteWorkflowProviderEventTriggerRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.PauseEventTrigger(ctx, &proto.PauseWorkflowProviderEventTriggerRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.ResumeEventTrigger(ctx, &proto.ResumeWorkflowProviderEventTriggerRequest{
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
		client, err := workflowFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		event, err := workflowEvent(input)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.PublishEvent(ctx, &proto.PublishWorkflowProviderEventRequest{
			Event:        event,
			ProviderName: input.ProviderName,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, workflowEventBody(result)), nil

	case "read_env":
		name, _ := params["name"].(string)
		val, ok := os.LookupEnv(name)
		body, _ := json.Marshal(map[string]any{"name": name, "value": val, "found": ok})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "read_file":
		path, _ := params["path"].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsPermission(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &gestalt.OperationResult{Status: http.StatusForbidden, Body: string(body)}, nil
			}
			if os.IsNotExist(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &gestalt.OperationResult{Status: http.StatusNotFound, Body: string(body)}, nil
			}
			body, _ := json.Marshal(map[string]any{"error": err.Error()})
			return &gestalt.OperationResult{Status: http.StatusInternalServerError, Body: string(body)}, nil
		}
		body, _ := json.Marshal(map[string]any{"content": string(data)})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

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
			return &gestalt.OperationResult{Status: http.StatusBadGateway, Body: string(body)}, nil
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		body, _ := json.Marshal(map[string]any{
			"status": resp.StatusCode,
			"body":   string(respBody),
		})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "indexeddb_roundtrip":
		binding, _ := params["binding"].(string)
		store, _ := params["store"].(string)
		id, _ := params["id"].(string)
		value, _ := params["value"].(string)

		var (
			db  gestalt.IndexedDBDatabase
			err error
		)
		if binding != "" {
			db, err = gestalt.IndexedDB(ctx, binding)
		} else {
			db, err = gestalt.IndexedDB(ctx)
		}
		if err != nil {
			return nil, err
		}
		defer func() { _ = db.Close() }()

		if _, err := db.CreateObjectStore(ctx, store, gestalt.ObjectStoreSchema{}); err != nil && !errors.Is(err, gestalt.ErrAlreadyExists) {
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
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

	case "s3_roundtrip":
		binding, _ := params["binding"].(string)
		key, _ := params["key"].(string)
		value, _ := params["value"].(string)

		var (
			client s3sdk.S3
			err    error
		)
		if binding != "" {
			client, err = gestalt.S3(ctx, binding)
		} else {
			client, err = gestalt.S3(ctx)
		}
		if err != nil {
			return nil, err
		}
		defer func() { _ = client.Close() }()

		obj := s3sdk.Object(client, key)
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
		page, err := client.ListObjects(ctx, gestalt.ListOptions{Prefix: key})
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
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

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

func workflowFromContext(ctx context.Context, invocationToken string) (gestalt.Workflow, error) {
	token := strings.TrimSpace(invocationToken)
	if token == "" {
		token = invocationTokenFromContext(ctx)
	}
	return gestalt.NewWorkflow(token)
}

func workflowTargetInput(target workflowScheduleTargetInput) (*proto.BoundWorkflowTarget, error) {
	input, err := workflowValueObject(target.Input)
	if err != nil {
		return nil, err
	}
	return &proto.BoundWorkflowTarget{
		Steps: []*proto.WorkflowStep{{
			Id: strings.TrimSpace(target.Operation),
			Action: &proto.WorkflowStep_App{App: &proto.WorkflowStepAppCall{
				Name:       target.App,
				Operation:  target.Operation,
				Connection: target.Connection,
				Instance:   target.Instance,
				Input:      input,
			}},
		}},
	}, nil
}

func workflowEventMatch(match workflowEventMatchInput) *proto.WorkflowEventMatch {
	return &proto.WorkflowEventMatch{
		Type:    match.Type,
		Source:  match.Source,
		Subject: match.Subject,
	}
}

func workflowEvent(input publishWorkflowEventInput) (*proto.WorkflowEvent, error) {
	data, err := structpb.NewStruct(anyMap(input.Data))
	if err != nil {
		return nil, err
	}
	extensions := make(map[string]*structpb.Value, len(input.Extensions))
	for key, value := range input.Extensions {
		field, err := structpb.NewValue(value)
		if err != nil {
			return nil, err
		}
		extensions[key] = field
	}
	event := &proto.WorkflowEvent{
		Id:              input.ID,
		Source:          input.Source,
		SpecVersion:     input.SpecVersion,
		Type:            input.Type,
		Subject:         input.Subject,
		Datacontenttype: input.DataContentType,
		Data:            data,
		Extensions:      extensions,
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

func managedWorkflowScheduleBody(value *proto.BoundWorkflowSchedule) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	body := map[string]any{
		"provider_name": value.GetProviderName(),
	}
	target := value.GetTarget()
	body["schedule"] = map[string]any{
		"id":          value.GetId(),
		"cron":        value.GetCron(),
		"timezone":    value.GetTimezone(),
		"paused":      value.GetPaused(),
		"created_at":  timestampBody(value.GetCreatedAt()),
		"updated_at":  timestampBody(value.GetUpdatedAt()),
		"next_run_at": timestampBody(value.GetNextRunAt()),
		"target": map[string]any{
			"app":        "",
			"operation":  "",
			"connection": "",
			"instance":   "",
			"input":      map[string]any{},
		},
	}
	if appTarget := workflowFirstAppStep(target); appTarget != nil {
		body["schedule"].(map[string]any)["target"] = map[string]any{
			"app":        appTarget.Name,
			"operation":  appTarget.Operation,
			"connection": appTarget.Connection,
			"instance":   appTarget.Instance,
			"input":      workflowAppStepInputMap(appTarget),
		}
	}
	return body
}

func managedWorkflowTriggerBody(value *proto.BoundWorkflowEventTrigger) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	body := map[string]any{
		"provider_name": value.GetProviderName(),
	}
	target := value.GetTarget()
	match := value.GetMatch()
	body["trigger"] = map[string]any{
		"id":         value.GetId(),
		"paused":     value.GetPaused(),
		"created_at": timestampBody(value.GetCreatedAt()),
		"updated_at": timestampBody(value.GetUpdatedAt()),
		"match": map[string]any{
			"type":    "",
			"source":  "",
			"subject": "",
		},
		"target": map[string]any{
			"app":        "",
			"operation":  "",
			"connection": "",
			"instance":   "",
			"input":      map[string]any{},
		},
	}
	if match != nil {
		body["trigger"].(map[string]any)["match"] = map[string]any{
			"type":    match.Type,
			"source":  match.Source,
			"subject": match.Subject,
		}
	}
	if appTarget := workflowFirstAppStep(target); appTarget != nil {
		body["trigger"].(map[string]any)["target"] = map[string]any{
			"app":        appTarget.Name,
			"operation":  appTarget.Operation,
			"connection": appTarget.Connection,
			"instance":   appTarget.Instance,
			"input":      workflowAppStepInputMap(appTarget),
		}
	}
	return body
}

func workflowFirstAppStep(target *proto.BoundWorkflowTarget) *proto.WorkflowStepAppCall {
	if target == nil || len(target.Steps) == 0 {
		return nil
	}
	return target.Steps[0].GetApp()
}

func workflowAppStepInputMap(target *proto.WorkflowStepAppCall) map[string]any {
	if target == nil || target.GetInput().GetObject() == nil {
		return map[string]any{}
	}
	fields := target.GetInput().GetObject().GetFields()
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = workflowValueToAny(value)
	}
	return out
}

func workflowValueObject(input map[string]any) (*proto.WorkflowValue, error) {
	out := make(map[string]*proto.WorkflowValue, len(input))
	for key, value := range input {
		field, err := workflowValueFromAny(value)
		if err != nil {
			return nil, err
		}
		out[key] = field
	}
	return &proto.WorkflowValue{
		Kind: &proto.WorkflowValue_Object{
			Object: &proto.WorkflowObject{Fields: out},
		},
	}, nil
}

func workflowValueFromAny(value any) (*proto.WorkflowValue, error) {
	switch typed := value.(type) {
	case map[string]any:
		return workflowValueObject(typed)
	case []any:
		out := make([]*proto.WorkflowValue, 0, len(typed))
		for _, value := range typed {
			field, err := workflowValueFromAny(value)
			if err != nil {
				return nil, err
			}
			out = append(out, field)
		}
		return &proto.WorkflowValue{
			Kind: &proto.WorkflowValue_Array{
				Array: &proto.WorkflowArray{Values: out},
			},
		}, nil
	default:
		literal, err := structpb.NewValue(value)
		if err != nil {
			return nil, err
		}
		return &proto.WorkflowValue{
			Kind: &proto.WorkflowValue_Literal{Literal: literal},
		}, nil
	}
}

func workflowValueToAny(value *proto.WorkflowValue) any {
	if value == nil {
		return nil
	}
	if literal := value.GetLiteral(); literal != nil {
		return literal.AsInterface()
	}
	if object := value.GetObject(); object != nil {
		out := make(map[string]any, len(object.GetFields()))
		for key, nested := range object.GetFields() {
			out[key] = workflowValueToAny(nested)
		}
		return out
	}
	if array := value.GetArray(); array != nil {
		out := make([]any, 0, len(array.GetValues()))
		for _, nested := range array.GetValues() {
			out = append(out, workflowValueToAny(nested))
		}
		return out
	}
	return nil
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
		"data": func() map[string]any {
			if value.GetData() == nil {
				return map[string]any{}
			}
			return value.GetData().AsMap()
		}(),
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

func anyMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func timeBody(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func timestampBody(value *timestamppb.Timestamp) any {
	if value == nil {
		return nil
	}
	return timeBody(value.AsTime())
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

func jsonResult(status int, body any) *gestalt.OperationResult {
	data, _ := json.Marshal(body)
	return &gestalt.OperationResult{Status: status, Body: string(data)}
}
