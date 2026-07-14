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
	"github.com/valon-technologies/gestalt/sdk/go/client"
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
	Provider     string                      `json:"provider" required:"true"`
	RunAs        string                      `json:"run_as,omitempty"`
	Target       workflowDefinitionStepInput `json:"target"`
	Activations  []workflowActivationInput   `json:"activations,omitempty"`
	Paused       bool                        `json:"paused,omitempty"`
}

type workflowDefinitionIDInput struct {
	DefinitionID string `json:"definition_id"`
	Provider     string `json:"provider" required:"true"`
}

type setWorkflowDefinitionPausedInput struct {
	DefinitionID string `json:"definition_id"`
	Provider     string `json:"provider" required:"true"`
	Paused       bool   `json:"paused"`
}

type setWorkflowActivationPausedInput struct {
	DefinitionID string `json:"definition_id"`
	ActivationID string `json:"activation_id"`
	Provider     string `json:"provider" required:"true"`
	Paused       bool   `json:"paused"`
}

type deliverWorkflowEventInput struct {
	ID              string         `json:"id,omitempty"`
	Provider        string         `json:"provider" required:"true"`
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
		invokeParams := input.Params
		if invokeParams == nil {
			invokeParams = map[string]any{}
		}
		result, err := invoker.InvokeRaw(ctx, &client.AppInvokeRequest{
			App:        input.App,
			Operation:  input.Operation,
			Connection: connection,
			Instance:   instance,
			Params:     invokeParams,
		})
		if err != nil {
			envelope["error"] = transportErrorString(err)
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
		result, err := invoker.InvokeGraphQL(ctx, input.App, input.Document, &client.AppInvokeGraphQLOptions{
			Connection: connection,
			Instance:   instance,
			Variables:  input.Variables,
		})
		if err != nil {
			envelope["error"] = transportErrorString(err)
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
		workflow, err := gestalt.WorkflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		target, err := workflowDefinitionTargetInput(input.Target)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		activations, err := workflowActivationsInput(input.Activations)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		providerName := strings.TrimSpace(input.Provider)
		if providerName == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "provider is required"}), nil
		}
		runAs := strings.TrimSpace(input.RunAs)
		if runAs == "" {
			runAs = "service_account:echo-workflow"
		}
		result, err := workflow.ApplyDefinition(ctx, providerName, gestalt.IdempotencyKeyFromContext(ctx), &client.WorkflowDefinitionSpec{
			Id:          input.DefinitionID,
			Target:      target,
			Activations: activations,
			Paused:      input.Paused,
			RunAs:       runAs,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": transportErrorString(err)}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "get_workflow_definition":
		input, err := decodeJSONParams[workflowDefinitionIDInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		workflow, err := gestalt.WorkflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		result, err := workflow.GetDefinition(ctx, input.Provider, input.DefinitionID)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": transportErrorString(err)}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "set_workflow_definition_paused":
		input, err := decodeJSONParams[setWorkflowDefinitionPausedInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		workflow, err := gestalt.WorkflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		result, err := workflow.SetDefinitionPaused(ctx, input.Provider, input.DefinitionID, input.Paused)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": transportErrorString(err)}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "set_workflow_activation_paused":
		input, err := decodeJSONParams[setWorkflowActivationPausedInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		workflow, err := gestalt.WorkflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		result, err := workflow.SetActivationPaused(ctx, input.Provider, input.DefinitionID, input.ActivationID, input.Paused)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": transportErrorString(err)}), nil
		}
		return jsonResult(http.StatusOK, workflowDefinitionBody(result)), nil

	case "delete_workflow_definition":
		input, err := decodeJSONParams[workflowDefinitionIDInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		workflow, err := gestalt.WorkflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		if err := workflow.DeleteDefinition(ctx, input.Provider, input.DefinitionID); err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": transportErrorString(err)}), nil
		}
		return jsonResult(http.StatusOK, map[string]any{"deleted": true, "definition_id": input.DefinitionID}), nil

	case "deliver_workflow_event":
		input, err := decodeJSONParams[deliverWorkflowEventInput](params)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		workflow, err := gestalt.WorkflowFromContext(ctx)
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": err.Error()}), nil
		}
		event, err := workflowEvent(input)
		if err != nil {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": err.Error()}), nil
		}
		providerName := strings.TrimSpace(input.Provider)
		if providerName == "" {
			return jsonResult(http.StatusBadRequest, map[string]any{"error": "provider is required"}), nil
		}
		result, err := workflow.DeliverEventRaw(ctx, &client.DeliverWorkflowProviderEventRequest{
			Provider: providerName,
			Event:    event,
		})
		if err != nil {
			return jsonResult(http.StatusOK, map[string]any{"error": transportErrorString(err)}), nil
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
		httpClient := &http.Client{}
		if proxyURL := os.Getenv("HTTP_PROXY"); proxyURL != "" {
			parsed, err := url.Parse(proxyURL)
			if err == nil {
				httpClient.Transport = &http.Transport{
					Proxy:           http.ProxyURL(parsed),
					TLSClientConfig: testTLSConfigFromEnv(),
				}
			}
		}
		resp, err := httpClient.Get(targetURL)
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

		s3c, err := client.ConnectS3(ctx, binding)
		if err != nil {
			return nil, err
		}

		writeStream, err := s3c.WriteObject(ctx, &client.WriteObjectOpen{
			Ref:         &client.S3ObjectRef{Key: key},
			ContentType: "text/plain",
		})
		if err != nil {
			return nil, err
		}
		if len(value) > 0 {
			if err := writeStream.Send([]byte(value)); err != nil && !errors.Is(err, io.EOF) {
				if _, recvErr := writeStream.CloseAndRecv(); recvErr != nil {
					return nil, recvErr
				}
				return nil, err
			}
		}
		if _, err := writeStream.CloseAndRecv(); err != nil {
			return nil, err
		}

		_, readStream, err := s3c.ReadObject(ctx, &client.ReadObjectRequest{
			Ref: &client.S3ObjectRef{Key: key},
		})
		if err != nil {
			return nil, err
		}
		var text strings.Builder
		for {
			chunk, err := readStream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			text.Write(chunk)
		}

		head, err := s3c.HeadObject(ctx, &client.S3ObjectRef{Key: key})
		if err != nil {
			return nil, err
		}
		stat := head.Meta

		page, err := s3c.ListObjects(ctx, key, "", "", "", 0)
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(page.Objects))
		for i := range page.Objects {
			keys = append(keys, page.Objects[i].Ref.Key)
		}
		body, _ := json.Marshal(map[string]any{
			"body":  text.String(),
			"key":   stat.Ref.Key,
			"size":  stat.Size,
			"keys":  keys,
			"type":  stat.ContentType,
			"etag":  stat.Etag,
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

func workflowDefinitionTargetInput(target workflowDefinitionStepInput) (*client.BoundWorkflowTarget, error) {
	return &client.BoundWorkflowTarget{
		Steps: []*client.WorkflowStep{{
			Id: strings.TrimSpace(target.Operation),
			Action: &client.WorkflowStepActionApp{Value: &client.WorkflowStepAppCall{
				Name:       target.App,
				Operation:  target.Operation,
				Connection: target.Connection,
				Instance:   target.Instance,
				Input:      workflowValueObject(target.Input),
			}},
		}},
	}, nil
}

func workflowEventMatch(match workflowEventMatchInput) *client.WorkflowEventMatch {
	return &client.WorkflowEventMatch{
		Type:    match.Type,
		Source:  match.Source,
		Subject: match.Subject,
	}
}

func workflowActivationsInput(values []workflowActivationInput) ([]*client.WorkflowActivation, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*client.WorkflowActivation, 0, len(values))
	for _, value := range values {
		activation := &client.WorkflowActivation{
			Id:     value.ID,
			Paused: value.Paused,
		}
		switch {
		case value.Schedule != nil && value.Event != nil:
			return nil, errors.New("workflow activation must set exactly one of schedule or event")
		case value.Schedule != nil:
			activation.Trigger = &client.WorkflowActivationTriggerSchedule{Value: &client.WorkflowScheduleActivation{
				Cron:     value.Schedule.Cron,
				Timezone: value.Schedule.Timezone,
			}}
		case value.Event != nil:
			activation.Trigger = &client.WorkflowActivationTriggerEvent{Value: &client.WorkflowEventActivation{
				Match: workflowEventMatch(value.Event.Match),
			}}
		default:
			return nil, errors.New("workflow activation must set schedule or event")
		}
		out = append(out, activation)
	}
	return out, nil
}

func workflowEvent(input deliverWorkflowEventInput) (*client.WorkflowEvent, error) {
	event := &client.WorkflowEvent{
		Id:              input.ID,
		Source:          input.Source,
		SpecVersion:     input.SpecVersion,
		Type:            input.Type,
		Subject:         input.Subject,
		Datacontenttype: input.DataContentType,
		Data:            input.Data,
		Extensions:      input.Extensions,
	}
	if strings.TrimSpace(input.Time) != "" {
		timestamp, err := time.Parse(time.RFC3339, input.Time)
		if err != nil {
			return nil, err
		}
		event.Time = &timestamp
	}
	return event, nil
}

func workflowDefinitionBody(value *client.WorkflowDefinition) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	body := map[string]any{
		"provider": value.Provider,
	}
	definition := map[string]any{
		"id":          value.Id,
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

func workflowActivationBodies(values []*client.WorkflowActivation) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, activation := range values {
		if activation == nil {
			continue
		}
		body := map[string]any{
			"id":     activation.Id,
			"paused": activation.Paused,
		}
		switch trigger := activation.Trigger.(type) {
		case *client.WorkflowActivationTriggerSchedule:
			if trigger.Value != nil {
				body["schedule"] = map[string]any{
					"cron":     trigger.Value.Cron,
					"timezone": trigger.Value.Timezone,
				}
			}
		case *client.WorkflowActivationTriggerEvent:
			if trigger.Value != nil && trigger.Value.Match != nil {
				body["event"] = map[string]any{
					"match": map[string]any{
						"type":    trigger.Value.Match.Type,
						"source":  trigger.Value.Match.Source,
						"subject": trigger.Value.Match.Subject,
					},
				}
			}
		}
		out = append(out, body)
	}
	return out
}

func workflowFirstAppStep(target *client.BoundWorkflowTarget) *client.WorkflowStepAppCall {
	if target == nil || len(target.Steps) == 0 || target.Steps[0] == nil {
		return nil
	}
	if action, ok := target.Steps[0].Action.(*client.WorkflowStepActionApp); ok {
		return action.Value
	}
	return nil
}

func workflowAppStepInputMap(target *client.WorkflowStepAppCall) map[string]any {
	if target == nil || target.Input == nil {
		return map[string]any{}
	}
	object, ok := target.Input.Kind.(*client.WorkflowValueKindObject)
	if !ok || object.Value == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(object.Value.Fields))
	for key, value := range object.Value.Fields {
		out[key] = workflowValueToAny(value)
	}
	return out
}

func workflowValueObject(input map[string]any) *client.WorkflowValue {
	if input == nil {
		return nil
	}
	fields := make(map[string]*client.WorkflowValue, len(input))
	for key, value := range input {
		fields[key] = workflowValueFromAny(value)
	}
	return &client.WorkflowValue{Kind: &client.WorkflowValueKindObject{Value: &client.WorkflowObject{Fields: fields}}}
}

func workflowValueFromAny(value any) *client.WorkflowValue {
	switch typed := value.(type) {
	case map[string]any:
		return workflowValueObject(typed)
	case []any:
		values := make([]*client.WorkflowValue, 0, len(typed))
		for _, item := range typed {
			values = append(values, workflowValueFromAny(item))
		}
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindArray{Value: &client.WorkflowArray{Values: values}}}
	default:
		return &client.WorkflowValue{Kind: &client.WorkflowValueKindLiteral{Value: value}}
	}
}

func workflowValueToAny(value *client.WorkflowValue) any {
	if value == nil {
		return nil
	}
	switch kind := value.Kind.(type) {
	case *client.WorkflowValueKindLiteral:
		return kind.Value
	case *client.WorkflowValueKindObject:
		if kind.Value == nil {
			return map[string]any{}
		}
		out := make(map[string]any, len(kind.Value.Fields))
		for key, nested := range kind.Value.Fields {
			out[key] = workflowValueToAny(nested)
		}
		return out
	case *client.WorkflowValueKindArray:
		if kind.Value == nil {
			return []any{}
		}
		out := make([]any, 0, len(kind.Value.Values))
		for _, nested := range kind.Value.Values {
			out = append(out, workflowValueToAny(nested))
		}
		return out
	default:
		return nil
	}
}

func workflowEventBody(value *client.WorkflowEvent) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                value.Id,
		"source":            value.Source,
		"spec_version":      value.SpecVersion,
		"type":              value.Type,
		"subject":           value.Subject,
		"time":              timeBody(value.Time),
		"data_content_type": value.Datacontenttype,
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

func timeBody(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value
}

// transportErrorString preserves the raw transport error text (including the
// gRPC "code = ..." prefix) that the pre-generated-client envelopes carried.
func transportErrorString(err error) string {
	if err == nil {
		return ""
	}
	var gestaltErr *client.GestaltError
	if errors.As(err, &gestaltErr) {
		if cause := gestaltErr.Unwrap(); cause != nil {
			return cause.Error()
		}
	}
	return err.Error()
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
