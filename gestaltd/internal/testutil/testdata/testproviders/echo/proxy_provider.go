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
)

type executableProvider interface {
	Configure(context.Context, string, map[string]any) error
	Execute(context.Context, string, map[string]any, string) (*gestalt.OperationResult, error)
}

type proxyProvider struct {
	inner executableProvider
}

type invokePluginInput struct {
	App        string         `json:"app"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
}

type invokePluginGraphQLInput struct {
	App        string         `json:"app"`
	Document   string         `json:"document"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Variables  map[string]any `json:"variables,omitempty"`
}

type workflowDefinitionStepInput struct {
	App        string         `json:"app"`
	Operation  string         `json:"operation"`
	Connection string         `json:"connection,omitempty"`
	Instance   string         `json:"instance,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
}

type workflowScheduleActivationInput struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone,omitempty"`
}

type workflowEventMatchInput struct {
	Type    string `json:"type"`
	Source  string `json:"source,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type workflowEventActivationInput struct {
	Match workflowEventMatchInput `json:"match"`
}

type workflowActivationInput struct {
	ID       string                           `json:"id"`
	Schedule *workflowScheduleActivationInput `json:"schedule,omitempty"`
	Event    *workflowEventActivationInput    `json:"event,omitempty"`
	Paused   bool                             `json:"paused,omitempty"`
}

type applyWorkflowDefinitionInput struct {
	DefinitionID string                      `json:"definition_id"`
	ProviderName string                      `json:"provider_name,omitempty"`
	Target       workflowDefinitionStepInput `json:"target"`
	Activations  []workflowActivationInput   `json:"activations,omitempty"`
	Paused       bool                        `json:"paused,omitempty"`
}

type workflowDefinitionIDInput struct {
	DefinitionID string `json:"definition_id"`
}

type setWorkflowDefinitionPausedInput struct {
	DefinitionID string `json:"definition_id"`
	Paused       bool   `json:"paused"`
}

type setWorkflowActivationPausedInput struct {
	DefinitionID string `json:"definition_id"`
	ActivationID string `json:"activation_id"`
	Paused       bool   `json:"paused"`
}

type deliverWorkflowEventInput struct {
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
	proxyOperation[applyWorkflowDefinitionInput]("apply_workflow_definition", http.MethodPost),
	proxyOperation[workflowDefinitionIDInput]("get_workflow_definition", http.MethodGet),
	proxyOperation[setWorkflowDefinitionPausedInput]("set_workflow_definition_paused", http.MethodPost),
	proxyOperation[setWorkflowActivationPausedInput]("set_workflow_activation_paused", http.MethodPost),
	proxyOperation[workflowDefinitionIDInput]("delete_workflow_definition", http.MethodPost),
	proxyOperation[deliverWorkflowEventInput]("deliver_workflow_event", http.MethodPost),
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
			result, err := p.Execute(ctx, id, params, req.Token)
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

		invoker, err := gestalt.AppFromContext(ctx)
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
		result, err := invoker.InvokeRaw(ctx, input.App, input.Operation, input.Params, opts)
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

		invoker, err := gestalt.AppFromContext(ctx)
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
		result, err := invoker.InvokeGraphQLRaw(ctx, input.App, input.Document, input.Variables, opts)
		if err != nil {
			envelope["error"] = err.Error()
			return jsonResult(http.StatusOK, envelope), nil
		}
		envelope["ok"] = true
		envelope["status"] = result.Status
		envelope["body"] = decodeResultBody(result.Body)
		return jsonResult(http.StatusOK, envelope), nil

	case "apply_workflow_definition":
		input, err := decodeJSONParams[applyWorkflowDefinitionInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		target, err := workflowDefinitionTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		activations, err := workflowActivationsInput(input.Activations)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.ApplyDefinition(ctx, gestalt.WorkflowApplyDefinition{
			ProviderName: input.ProviderName,
			Spec: &gestalt.WorkflowDefinitionSpec{
				ID:          input.DefinitionID,
				Target:      target,
				Activations: activations,
				Paused:      input.Paused,
			},
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "get_workflow_definition":
		input, err := decodeJSONParams[workflowDefinitionIDInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.GetDefinition(ctx, gestalt.WorkflowGetDefinition{
			DefinitionID: input.DefinitionID,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "set_workflow_definition_paused":
		input, err := decodeJSONParams[setWorkflowDefinitionPausedInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.SetDefinitionPaused(ctx, gestalt.WorkflowSetDefinitionPaused{
			DefinitionID: input.DefinitionID,
			Paused:       input.Paused,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "set_workflow_activation_paused":
		input, err := decodeJSONParams[setWorkflowActivationPausedInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		result, err := client.SetActivationPaused(ctx, gestalt.WorkflowSetActivationPaused{
			DefinitionID: input.DefinitionID,
			ActivationID: input.ActivationID,
			Paused:       input.Paused,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "delete_workflow_definition":
		input, err := decodeJSONParams[workflowDefinitionIDInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		if err := client.DeleteDefinition(ctx, gestalt.WorkflowDeleteDefinition{
			DefinitionID: input.DefinitionID,
		}); err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		return jsonResult(http.StatusOK, map[string]any{"deleted": true, "definition_id": input.DefinitionID}), nil

	case "deliver_workflow_event":
		input, err := decodeJSONParams[deliverWorkflowEventInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		client, err := workflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		defer func() { _ = client.Close() }()
		event, err := workflowEvent(input)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		result, err := client.DeliverEvent(ctx, gestalt.WorkflowDeliverEvent{
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
		return &gestalt.OperationResult{Status: http.StatusOK, Body: body}, nil

	case "read_file":
		path, _ := params["path"].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsPermission(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &gestalt.OperationResult{Status: http.StatusForbidden, Body: body}, nil
			}
			if os.IsNotExist(err) {
				body, _ := json.Marshal(map[string]any{"error": err.Error()})
				return &gestalt.OperationResult{Status: http.StatusNotFound, Body: body}, nil
			}
			body, _ := json.Marshal(map[string]any{"error": err.Error()})
			return &gestalt.OperationResult{Status: http.StatusInternalServerError, Body: body}, nil
		}
		body, _ := json.Marshal(map[string]any{"content": string(data)})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: body}, nil

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
			return &gestalt.OperationResult{Status: http.StatusBadGateway, Body: body}, nil
		}
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		body, _ := json.Marshal(map[string]any{
			"status": resp.StatusCode,
			"body":   string(respBody),
		})
		return &gestalt.OperationResult{Status: http.StatusOK, Body: body}, nil

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

		if _, err := db.CreateObjectStore(ctx, store, gestalt.ObjectStoreOptions{}); err != nil && !errors.Is(err, gestalt.ErrAlreadyExists) {
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
		return &gestalt.OperationResult{Status: http.StatusOK, Body: body}, nil

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
		if _, err := obj.WriteString(ctx, value, &gestalt.WriteRequest{ContentType: "text/plain"}); err != nil {
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
		page, err := client.ListObjects(ctx, gestalt.ListRequest{Prefix: key})
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
		return &gestalt.OperationResult{Status: http.StatusOK, Body: body}, nil

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

func workflowFromContext(ctx context.Context) (gestalt.Workflow, error) {
	return gestalt.WorkflowFromContext(ctx)
}

func workflowDefinitionTargetInput(target workflowDefinitionStepInput) (*gestalt.BoundWorkflowTarget, error) {
	return &gestalt.BoundWorkflowTarget{
		Steps: []gestalt.WorkflowStep{{
			ID: strings.TrimSpace(target.Operation),
			App: &gestalt.WorkflowStepAppCall{
				Name:       target.App,
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

func workflowActivationsInput(values []workflowActivationInput) ([]gestalt.WorkflowActivation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]gestalt.WorkflowActivation, 0, len(values))
	for _, value := range values {
		activation := gestalt.WorkflowActivation{
			ID:     value.ID,
			Paused: value.Paused,
		}
		switch {
		case value.Schedule != nil && value.Event != nil:
			return nil, errors.New("workflow activation must set exactly one of schedule or event")
		case value.Schedule != nil:
			activation.Schedule = &gestalt.WorkflowScheduleActivation{
				Cron:     value.Schedule.Cron,
				Timezone: value.Schedule.Timezone,
			}
		case value.Event != nil:
			activation.Event = &gestalt.WorkflowEventActivation{
				Match: workflowEventMatch(value.Event.Match),
			}
		default:
			return nil, errors.New("workflow activation must set schedule or event")
		}
		out = append(out, activation)
	}
	return out, nil
}

func workflowEvent(input deliverWorkflowEventInput) (*gestalt.WorkflowEvent, error) {
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

func workflowDefinitionBody(value *gestalt.WorkflowDefinition) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	body := map[string]any{
		"provider_name": value.ProviderName,
	}
	definition := map[string]any{
		"id":          value.ID,
		"generation":  value.Generation,
		"paused":      value.Paused,
		"created_at":  timeBody(value.CreatedAt),
		"updated_at":  timeBody(value.UpdatedAt),
		"activations": workflowActivationBodies(value.Activations),
		"target": map[string]any{
			"app":        "",
			"operation":  "",
			"connection": "",
			"instance":   "",
			"input":      map[string]any{},
		},
	}
	if appTarget := workflowFirstAppStep(value.Target); appTarget != nil {
		definition["target"] = map[string]any{
			"app":        appTarget.Name,
			"operation":  appTarget.Operation,
			"connection": appTarget.Connection,
			"instance":   appTarget.Instance,
			"input":      workflowAppStepInputMap(appTarget),
		}
	}
	body["definition"] = definition
	return body
}

func workflowActivationBodies(values []gestalt.WorkflowActivation) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, activation := range values {
		body := map[string]any{
			"id":     activation.ID,
			"paused": activation.Paused,
		}
		if activation.Schedule != nil {
			body["schedule"] = map[string]any{
				"cron":     activation.Schedule.Cron,
				"timezone": activation.Schedule.Timezone,
			}
		}
		if activation.Event != nil && activation.Event.Match != nil {
			body["event"] = map[string]any{
				"match": map[string]any{
					"type":    activation.Event.Match.Type,
					"source":  activation.Event.Match.Source,
					"subject": activation.Event.Match.Subject,
				},
			}
		}
		out = append(out, body)
	}
	return out
}

func workflowFirstAppStep(target *gestalt.BoundWorkflowTarget) *gestalt.WorkflowStepAppCall {
	if target == nil || len(target.Steps) == 0 {
		return nil
	}
	return target.Steps[0].App
}

func workflowAppStepInputMap(target *gestalt.WorkflowStepAppCall) map[string]any {
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

func decodeResultBody(body []byte) any {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err == nil {
		return decoded
	}
	return string(body)
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
	return &gestalt.OperationResult{Status: status, Body: data}
}
