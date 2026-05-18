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
	Bucket  string `json:"bucket" required:"true"`
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
			invocationToken = invocationTokenFromContext(ctx)
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
			invocationToken = invocationTokenFromContext(ctx)
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
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.CreateSchedule(ctx, gestalt.WorkflowManagerCreateSchedule{
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
		result, err := client.GetSchedule(ctx, gestalt.WorkflowManagerGetSchedule{
			ScheduleID: input.ScheduleID,
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
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpdateSchedule(ctx, gestalt.WorkflowManagerUpdateSchedule{
			ScheduleID:   input.ScheduleID,
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
		if err := client.DeleteSchedule(ctx, gestalt.WorkflowManagerDeleteSchedule{
			ScheduleID: input.ScheduleID,
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
		result, err := client.PauseSchedule(ctx, gestalt.WorkflowManagerPauseSchedule{
			ScheduleID: input.ScheduleID,
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
		result, err := client.ResumeSchedule(ctx, gestalt.WorkflowManagerResumeSchedule{
			ScheduleID: input.ScheduleID,
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
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.CreateTrigger(ctx, gestalt.WorkflowManagerCreateEventTrigger{
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
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.GetTrigger(ctx, gestalt.WorkflowManagerGetEventTrigger{
			TriggerID: input.TriggerID,
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
		target, err := workflowTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.UpdateTrigger(ctx, gestalt.WorkflowManagerUpdateEventTrigger{
			TriggerID:    input.TriggerID,
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
		client, err := workflowManagerFromContext(ctx, input.InvocationToken)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		if err := client.DeleteTrigger(ctx, gestalt.WorkflowManagerDeleteEventTrigger{
			TriggerID: input.TriggerID,
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
		result, err := client.PauseTrigger(ctx, gestalt.WorkflowManagerPauseEventTrigger{
			TriggerID: input.TriggerID,
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
		result, err := client.ResumeTrigger(ctx, gestalt.WorkflowManagerResumeEventTrigger{
			TriggerID: input.TriggerID,
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
		event, err := workflowEvent(input)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.PublishEvent(ctx, gestalt.WorkflowManagerPublishEvent{
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
		return &gestalt.OperationResult{Status: http.StatusOK, Body: string(body)}, nil

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

func workflowManagerFromContext(ctx context.Context, invocationToken string) (*gestalt.WorkflowManagerClient, error) {
	token := strings.TrimSpace(invocationToken)
	if token == "" {
		token = invocationTokenFromContext(ctx)
	}
	return gestalt.WorkflowManager(token)
}

func workflowTargetInput(target workflowScheduleTargetInput) (*gestalt.BoundWorkflowTarget, error) {
	return &gestalt.BoundWorkflowTarget{
		Steps: []gestalt.WorkflowStep{{
			ID: strings.TrimSpace(target.Operation),
			Plugin: &gestalt.WorkflowStepPluginCall{
				Name:       target.Plugin,
				Operation:  target.Operation,
				Connection: target.Connection,
				Instance:   target.Instance,
				Input:      workflowValueObject(target.Input),
			},
		}},
	}, nil
}

func workflowEventMatch(match workflowEventMatchInput) *gestalt.WorkflowEventMatch {
	return &gestalt.WorkflowEventMatch{
		Type:    match.Type,
		Source:  match.Source,
		Subject: match.Subject,
	}
}

func workflowEvent(input publishWorkflowEventInput) (*gestalt.WorkflowEvent, error) {
	event := &gestalt.WorkflowEvent{
		ID:              input.ID,
		Source:          input.Source,
		SpecVersion:     input.SpecVersion,
		Type:            input.Type,
		Subject:         input.Subject,
		DataContentType: input.DataContentType,
		Data:            input.Data,
		Extensions:      input.Extensions,
	}
	if strings.TrimSpace(input.Time) != "" {
		timestamp, err := time.Parse(time.RFC3339, input.Time)
		if err != nil {
			return nil, err
		}
		event.Time = timestamp
	}
	return event, nil
}

func managedWorkflowScheduleBody(value *gestalt.WorkflowManagerSchedule) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	schedule := value.Schedule
	body := map[string]any{
		"provider_name": value.ProviderName,
	}
	if schedule == nil {
		return body
	}
	target := schedule.Target
	body["schedule"] = map[string]any{
		"id":         schedule.ID,
		"cron":       schedule.Cron,
		"timezone":   schedule.Timezone,
		"paused":     schedule.Paused,
		"created_at": timeBody(schedule.CreatedAt),
		"updated_at": timeBody(schedule.UpdatedAt),
		"next_run_at": func() any {
			if schedule.NextRunAt == nil {
				return nil
			}
			return *schedule.NextRunAt
		}(),
		"target": map[string]any{
			"plugin":     "",
			"operation":  "",
			"connection": "",
			"instance":   "",
			"input":      map[string]any{},
		},
	}
	if pluginTarget := workflowFirstPluginStep(target); pluginTarget != nil {
		body["schedule"].(map[string]any)["target"] = map[string]any{
			"plugin":     pluginTarget.Name,
			"operation":  pluginTarget.Operation,
			"connection": pluginTarget.Connection,
			"instance":   pluginTarget.Instance,
			"input":      workflowPluginTargetInputMap(pluginTarget),
		}
	}
	return body
}

func managedWorkflowTriggerBody(value *gestalt.WorkflowManagerEventTrigger) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	trigger := value.Trigger
	body := map[string]any{
		"provider_name": value.ProviderName,
	}
	if trigger == nil {
		return body
	}
	target := trigger.Target
	match := trigger.Match
	body["trigger"] = map[string]any{
		"id":         trigger.ID,
		"paused":     trigger.Paused,
		"created_at": timeBody(trigger.CreatedAt),
		"updated_at": timeBody(trigger.UpdatedAt),
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
			"type":    match.Type,
			"source":  match.Source,
			"subject": match.Subject,
		}
	}
	if pluginTarget := workflowFirstPluginStep(target); pluginTarget != nil {
		body["trigger"].(map[string]any)["target"] = map[string]any{
			"plugin":     pluginTarget.Name,
			"operation":  pluginTarget.Operation,
			"connection": pluginTarget.Connection,
			"instance":   pluginTarget.Instance,
			"input":      workflowPluginTargetInputMap(pluginTarget),
		}
	}
	return body
}

func workflowFirstPluginStep(target *gestalt.BoundWorkflowTarget) *gestalt.WorkflowStepPluginCall {
	if target == nil || len(target.Steps) == 0 {
		return nil
	}
	return target.Steps[0].Plugin
}

func workflowPluginTargetInputMap(target *gestalt.WorkflowStepPluginCall) map[string]any {
	if target == nil || target.Input.Object == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(target.Input.Object))
	for key, value := range target.Input.Object {
		out[key] = workflowValueToAny(value)
	}
	return out
}

func workflowValueObject(input map[string]any) gestalt.WorkflowValue {
	if input == nil {
		return gestalt.WorkflowValue{}
	}
	out := make(map[string]gestalt.WorkflowValue, len(input))
	for key, value := range input {
		out[key] = workflowValueFromAny(value)
	}
	return gestalt.WorkflowValue{Object: out}
}

func workflowValueFromAny(value any) gestalt.WorkflowValue {
	switch typed := value.(type) {
	case map[string]any:
		return workflowValueObject(typed)
	case []any:
		out := make([]gestalt.WorkflowValue, 0, len(typed))
		for _, value := range typed {
			out = append(out, workflowValueFromAny(value))
		}
		return gestalt.WorkflowValue{Array: out}
	default:
		return gestalt.WorkflowValue{Literal: value, LiteralSet: true}
	}
}

func workflowValueToAny(value gestalt.WorkflowValue) any {
	switch {
	case value.LiteralSet:
		return value.Literal
	case value.Object != nil:
		out := make(map[string]any, len(value.Object))
		for key, nested := range value.Object {
			out[key] = workflowValueToAny(nested)
		}
		return out
	case value.Array != nil:
		out := make([]any, 0, len(value.Array))
		for _, nested := range value.Array {
			out = append(out, workflowValueToAny(nested))
		}
		return out
	default:
		return nil
	}
}

func workflowEventBody(value *gestalt.WorkflowEvent) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                value.ID,
		"source":            value.Source,
		"spec_version":      value.SpecVersion,
		"type":              value.Type,
		"subject":           value.Subject,
		"time":              timeBody(value.Time),
		"data_content_type": value.DataContentType,
		"data":              anyMap(value.Data),
		"extensions": func() map[string]any {
			if len(value.Extensions) == 0 {
				return map[string]any{}
			}
			out := make(map[string]any, len(value.Extensions))
			for key, field := range value.Extensions {
				if field != nil {
					out[key] = field
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
