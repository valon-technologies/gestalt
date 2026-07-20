package gestalt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

// Request carries execution-scoped metadata into typed handlers.
type Request struct {
	Token            string
	ConnectionParams map[string]string
	Subject          Subject
	AgentSubject     Subject
	Credential       Credential
	Access           Access
	Host             Host
	Caller           RequestCaller
	WorkflowContext  map[string]any
	IdempotencyKey   string
	ToolRefs         []AgentToolRef
	ToolRefsSet      bool
	requestContext   *proto.RequestContext
}

// RequestCallerKind identifies the trusted provider surface making a host
// service request. These values match the daemon invocation caller kinds.
type RequestCallerKind string

// The caller kinds a routed request can carry.
const (
	RequestCallerKindApp      RequestCallerKind = "app"
	RequestCallerKindWorkflow RequestCallerKind = "workflow"
	RequestCallerKindAgent    RequestCallerKind = "agent"
)

// RequestCaller identifies the trusted provider making a host service request.
type RequestCaller struct {
	Kind RequestCallerKind
	Name string
}

// RequestInput contains the public request authority fields used to construct
// an SDK Request and its wire RequestContext together.
type RequestInput struct {
	Token            string
	ConnectionParams map[string]string
	Subject          Subject
	AgentSubject     Subject
	Credential       Credential
	Access           Access
	Host             Host
	Caller           RequestCaller
	WorkflowContext  map[string]any
	IdempotencyKey   string
	ToolRefs         []AgentToolRef
	ToolRefsSet      bool
}

// NewRequest builds a Request with a canonical host-service RequestContext.
func NewRequest(input RequestInput) (Request, error) {
	req := Request{
		Token:            input.Token,
		ConnectionParams: cloneStringMap(input.ConnectionParams),
		Subject:          input.Subject,
		AgentSubject:     input.AgentSubject,
		Credential:       input.Credential,
		Access:           input.Access,
		Host:             input.Host,
		Caller:           RequestCaller{Kind: RequestCallerKind(strings.TrimSpace(string(input.Caller.Kind))), Name: strings.TrimSpace(input.Caller.Name)},
		IdempotencyKey:   input.IdempotencyKey,
		ToolRefs:         copyAgentToolRefs(input.ToolRefs),
		ToolRefsSet:      input.ToolRefsSet,
	}
	reqCtx, err := requestContextForRequest(req)
	if err != nil {
		return Request{}, err
	}
	if input.WorkflowContext != nil {
		workflow, err := structFromAny(input.WorkflowContext)
		if err != nil {
			return Request{}, fmt.Errorf("request: encode workflow context: %w", err)
		}
		if workflow != nil {
			if reqCtx == nil {
				reqCtx = &proto.RequestContext{}
			}
			reqCtx.Workflow = workflow
			req.WorkflowContext = mapFromStruct(workflow)
		}
	}
	if emptyRequestContext(reqCtx) {
		reqCtx = nil
	}
	req.requestContext = reqCtx
	return req, nil
}

// ConnectionParam returns one resolved connection parameter by name and whether
// it was present in the request.
func (r Request) ConnectionParam(name string) (string, bool) {
	if r.ConnectionParams == nil {
		return "", false
	}
	value, ok := r.ConnectionParams[name]
	return value, ok
}

// RequestFromContext reconstructs the current provider request from ctx.
func RequestFromContext(ctx context.Context) Request {
	reqCtx := requestContextFromContext(ctx)
	caller := RequestCaller{}
	if protoCaller := reqCtx.GetCaller(); protoCaller != nil {
		caller = RequestCaller{
			Kind: RequestCallerKind(strings.TrimSpace(protoCaller.GetKind())),
			Name: strings.TrimSpace(protoCaller.GetName()),
		}
	}
	workflow := WorkflowContextFromContext(ctx)
	if workflow == nil && reqCtx.GetWorkflow() != nil {
		workflow = reqCtx.GetWorkflow().AsMap()
	}
	return Request{
		ConnectionParams: cloneStringMap(ConnectionParams(ctx)),
		Subject:          SubjectFromContext(ctx),
		AgentSubject:     AgentSubjectFromContext(ctx),
		Credential:       CredentialFromContext(ctx),
		Access:           AccessFromContext(ctx),
		Host:             HostContextFromContext(ctx),
		Caller:           caller,
		WorkflowContext:  cloneWorkflowContextMap(workflow),
		IdempotencyKey:   IdempotencyKeyFromContext(ctx),
		ToolRefs:         ToolRefsFromContext(ctx),
		ToolRefsSet:      ToolRefsSetFromContext(ctx),
		requestContext:   reqCtx,
	}
}

func requestContextForRequest(req Request) (*proto.RequestContext, error) {
	reqCtx := cloneRequestContext(req.requestContext)
	if reqCtx == nil {
		reqCtx = &proto.RequestContext{}
	}
	if reqCtx.Subject == nil && !emptySubject(req.Subject) {
		reqCtx.Subject = subjectContextFromSubject(req.Subject)
	}
	if reqCtx.AgentSubject == nil && !emptySubject(req.AgentSubject) {
		reqCtx.AgentSubject = subjectContextFromSubject(req.AgentSubject)
	}
	if reqCtx.Credential == nil && !emptyCredential(req.Credential) {
		reqCtx.Credential = &proto.CredentialContext{
			Mode:       req.Credential.Mode,
			SubjectId:  req.Credential.SubjectID,
			Connection: req.Credential.Connection,
			Instance:   req.Credential.Instance,
		}
	}
	if reqCtx.Access == nil && !emptyAccess(req.Access) {
		reqCtx.Access = &proto.AccessContext{Policy: req.Access.Policy, Role: req.Access.Role}
	}
	if reqCtx.Host == nil && req.Host.PublicBaseURL != "" {
		reqCtx.Host = &proto.HostContext{PublicBaseUrl: req.Host.PublicBaseURL}
	}
	if reqCtx.Caller == nil {
		callerKind := strings.TrimSpace(string(req.Caller.Kind))
		callerName := strings.TrimSpace(req.Caller.Name)
		if callerKind != "" || callerName != "" {
			if callerKind == "" || callerName == "" {
				return nil, fmt.Errorf("request: caller kind and name are required together")
			}
			reqCtx.Caller = &proto.ProviderContext{Kind: callerKind, Name: callerName}
		}
	}
	if reqCtx.Workflow == nil && req.WorkflowContext != nil {
		workflow, err := structFromAny(req.WorkflowContext)
		if err != nil {
			return nil, fmt.Errorf("request: encode workflow context: %w", err)
		}
		reqCtx.Workflow = workflow
	}
	if !reqCtx.ToolRefsSet && req.ToolRefsSet {
		reqCtx.ToolRefsSet = true
		reqCtx.ToolRefs = agentToolRefsToProto(req.ToolRefs)
	}
	if emptyRequestContext(reqCtx) {
		return nil, nil
	}
	return reqCtx, nil
}

func subjectContextFromSubject(subject Subject) *proto.SubjectContext {
	return &proto.SubjectContext{
		Id:          subject.ID,
		Email:       subject.Email,
		DisplayName: subject.DisplayName,
		Scopes:      cloneStrings(subject.Scopes),
		Permissions: subjectPermissionsToProto(subject.Permissions),
	}
}

func emptySubject(subject Subject) bool {
	return subject.ID == "" && subject.Email == "" && subject.DisplayName == "" && len(subject.Scopes) == 0 && len(subject.Permissions) == 0
}

func subjectPermissionsToProto(values []SubjectPermission) []*proto.SubjectPermissionContext {
	if len(values) == 0 {
		return nil
	}
	out := make([]*proto.SubjectPermissionContext, 0, len(values))
	for _, value := range values {
		app := strings.TrimSpace(value.App)
		if app == "" {
			continue
		}
		permission := &proto.SubjectPermissionContext{App: app}
		if len(value.Operations) == 0 {
			permission.AllOperations = true
		} else {
			permission.Operations = cloneStrings(value.Operations)
		}
		out = append(out, permission)
	}
	return out
}

func subjectPermissionsFromProto(values []*proto.SubjectPermissionContext) []SubjectPermission {
	if len(values) == 0 {
		return nil
	}
	out := make([]SubjectPermission, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		app := strings.TrimSpace(value.GetApp())
		if app == "" {
			continue
		}
		permission := SubjectPermission{App: app}
		if !value.GetAllOperations() {
			permission.Operations = cloneStrings(value.GetOperations())
		}
		out = append(out, permission)
	}
	return out
}

func emptyCredential(credential Credential) bool {
	return credential.Mode == "" && credential.SubjectID == "" && credential.Connection == "" && credential.Instance == ""
}

func emptyAccess(access Access) bool {
	return access.Policy == "" && access.Role == ""
}

func cloneWorkflowContextMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func emptyRequestContext(reqCtx *proto.RequestContext) bool {
	return reqCtx == nil ||
		reqCtx.Subject == nil &&
			reqCtx.AgentSubject == nil &&
			reqCtx.Credential == nil &&
			reqCtx.Access == nil &&
			reqCtx.Host == nil &&
			reqCtx.Workflow == nil &&
			!reqCtx.ToolRefsSet &&
			reqCtx.Caller == nil &&
			reqCtx.Invocation == nil &&
			reqCtx.RequestMeta == nil
}

// Response is the typed handler result marshaled into the provider response body.
// A zero Status defaults to 200.
type Response[T any] struct {
	Status  int
	Headers http.Header
	Body    T
}

// OK returns a typed JSON response with status 200.
func OK[T any](body T) Response[T] {
	return Response[T]{Status: http.StatusOK, Body: body}
}

// Operation describes one statically declared executable operation.
// Input and output types are used for typed dispatch and catalog generation.
type Operation[In any, Out any] struct {
	ID           string
	Method       string
	Title        string
	Description  string
	AllowedRoles []string
	Tags         []string
	ReadOnly     bool
	Visible      *bool
}

// Registration describes one provider registered with the router.
type Registration[P any] struct {
	catalogOp     *CatalogOperation
	execute       func(context.Context, *P, map[string]any, Request) (*OperationResult, error)
	streamExecute func(context.Context, *P, map[string]any, Request) (StreamReader, error)
	err           error
}

// Register ties a typed operation definition to a typed handler.
//
// Typical usage looks like:
//
//	var Router = gestalt.MustRouter(
//		gestalt.Register(listWidgets, (*Provider).ListWidgets),
//	)
func Register[P any, In any, Out any](
	op Operation[In, Out],
	handler func(*P, context.Context, In, Request) (Response[Out], error),
) Registration[P] {
	catOp, err := catalogOperationFor(op)
	if err != nil {
		return Registration[P]{err: err}
	}
	return Registration[P]{
		catalogOp: catOp,
		execute: func(ctx context.Context, provider *P, rawParams map[string]any, req Request) (*OperationResult, error) {
			var input In
			if err := decodeParams(rawParams, &input); err != nil {
				return nil, newOperationError(http.StatusBadRequest, fmt.Sprintf("decode params for %q: %v", op.ID, err), err)
			}

			resp, err := handler(provider, ctx, input, req)
			if err != nil {
				return nil, err
			}

			status := resp.Status
			if status == 0 {
				status = http.StatusOK
			}
			body, err := json.Marshal(resp.Body)
			if err != nil {
				return nil, newOperationError(http.StatusInternalServerError, fmt.Sprintf("marshal response for %q: %v", op.ID, err), err)
			}
			return &OperationResult{Status: status, Headers: jsonResponseHeaders(resp.Headers), Body: body}, nil
		},
	}
}

// StreamOperation describes one statically declared streaming operation.
// Input is used for typed dispatch; the handler yields InvokeFrame frames
// (metadata first, then data). The catalog response is emitted as a stream
// spec with the given media type.
type StreamOperation[In any] struct {
	ID           string
	Method       string
	Title        string
	Description  string
	MediaType    string
	ItemSchema   string
	AllowedRoles []string
	Tags         []string
	ReadOnly     bool
	Visible      *bool
}

// RegisterStream ties a streaming operation definition to a typed handler
// that yields InvokeFrame frames. The handler is responsible for emitting a
// leading metadata frame and subsequent data frames.
func RegisterStream[P any, In any](
	op StreamOperation[In],
	handler func(*P, context.Context, In, Request) (StreamReader, error),
) Registration[P] {
	catOp, err := streamCatalogOperationFor(op)
	if err != nil {
		return Registration[P]{err: err}
	}
	return Registration[P]{
		catalogOp: catOp,
		streamExecute: func(ctx context.Context, provider *P, rawParams map[string]any, req Request) (StreamReader, error) {
			var input In
			if err := decodeParams(rawParams, &input); err != nil {
				return nil, newOperationError(http.StatusBadRequest, fmt.Sprintf("decode params for %q: %v", op.ID, err), err)
			}
			return handler(provider, ctx, input, req)
		},
	}
}

// streamCatalogOperationFor builds a CatalogOperation whose Response is a
// StreamResponseSpec.
func streamCatalogOperationFor[In any](op StreamOperation[In]) (*CatalogOperation, error) {
	id := strings.TrimSpace(op.ID)
	if id == "" {
		return nil, fmt.Errorf("operation id is required")
	}
	params, err := catalogParametersFor[In]()
	if err != nil {
		return nil, fmt.Errorf("operation %q: %w", id, err)
	}
	catOp := &CatalogOperation{
		Id:           id,
		Method:       normalizeMethod(op.Method),
		Title:        strings.TrimSpace(op.Title),
		Description:  strings.TrimSpace(op.Description),
		AllowedRoles: append([]string(nil), op.AllowedRoles...),
		Parameters:   params,
		Tags:         append([]string(nil), op.Tags...),
		ReadOnly:     op.ReadOnly,
		Response: &OperationResponseSpec{
			Stream: &StreamResponseSpec{
				MediaType:  strings.TrimSpace(op.MediaType),
				ItemSchema: strings.TrimSpace(op.ItemSchema),
			},
		},
	}
	if op.Visible != nil {
		catOp.Visible = op.Visible
	}
	return catOp, nil
}

// Router dispatches provider Execute calls against typed handlers and derives
// the corresponding static executable catalog.
type Router[P any] struct {
	catalog        *Catalog
	handlers       map[string]func(context.Context, *P, map[string]any, Request) (*OperationResult, error)
	streamHandlers map[string]func(context.Context, *P, map[string]any, Request) (StreamReader, error)
}

// NewRouter constructs a typed router from registrations. Source-provider flows
// derive the router name from manifest.yaml at build time.
func NewRouter[P any](registrations ...Registration[P]) (*Router[P], error) {
	return newRouter("", registrations...)
}

func newRouter[P any](name string, registrations ...Registration[P]) (*Router[P], error) {
	router := &Router[P]{
		catalog: &Catalog{
			Name:       name,
			Operations: make([]*CatalogOperation, 0, len(registrations)),
		},
		handlers:       make(map[string]func(context.Context, *P, map[string]any, Request) (*OperationResult, error), len(registrations)),
		streamHandlers: make(map[string]func(context.Context, *P, map[string]any, Request) (StreamReader, error)),
	}
	for i := range registrations {
		reg := registrations[i]
		if reg.err != nil {
			return nil, reg.err
		}
		opID := reg.catalogOp.GetId()
		if _, exists := router.handlers[opID]; exists {
			return nil, fmt.Errorf("duplicate operation id %q", opID)
		}
		if _, exists := router.streamHandlers[opID]; exists {
			return nil, fmt.Errorf("duplicate operation id %q", opID)
		}
		if reg.streamExecute != nil {
			router.streamHandlers[opID] = reg.streamExecute
		} else {
			router.handlers[opID] = reg.execute
		}
		router.catalog.Operations = append(router.catalog.Operations, reg.catalogOp)
	}
	return router, nil
}

// MustRouter panics if [NewRouter] returns an error.
func MustRouter[P any](registrations ...Registration[P]) *Router[P] {
	router, err := NewRouter(registrations...)
	if err != nil {
		panic(err)
	}
	return router
}

// Catalog returns a defensive copy of the router's derived static catalog.
func (r *Router[P]) Catalog() *Catalog {
	if r == nil {
		return nil
	}
	return cloneCatalog(r.catalog)
}

// WithName returns a copy of r with the catalog name overridden.
func (r *Router[P]) WithName(name string) *Router[P] {
	if r == nil {
		return nil
	}
	cat := cloneCatalog(r.catalog)
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		cat.Name = trimmed
	}
	handlers := make(map[string]func(context.Context, *P, map[string]any, Request) (*OperationResult, error), len(r.handlers))
	for opID, handler := range r.handlers {
		handlers[opID] = handler
	}
	streamHandlers := make(map[string]func(context.Context, *P, map[string]any, Request) (StreamReader, error), len(r.streamHandlers))
	for opID, handler := range r.streamHandlers {
		streamHandlers[opID] = handler
	}
	return &Router[P]{
		catalog:        cat,
		handlers:       handlers,
		streamHandlers: streamHandlers,
	}
}

// Execute decodes params into the typed input struct and dispatches the named
// operation.
func (r *Router[P]) Execute(ctx context.Context, provider *P, operation string, params map[string]any, token string) (*OperationResult, error) {
	if r == nil {
		return operationResult(http.StatusInternalServerError, routerNilMessage), nil
	}
	handler, ok := r.handlers[operation]
	if !ok {
		return operationResult(http.StatusNotFound, unknownOperationMessage), nil
	}
	result := protectedOperationResult(operation, func() (*OperationResult, error) {
		return handler(ctx, provider, params, Request{
			Token:            token,
			ConnectionParams: ConnectionParams(ctx),
			Subject:          SubjectFromContext(ctx),
			AgentSubject:     AgentSubjectFromContext(ctx),
			Credential:       CredentialFromContext(ctx),
			Access:           AccessFromContext(ctx),
			Host:             HostContextFromContext(ctx),
			IdempotencyKey:   IdempotencyKeyFromContext(ctx),
			ToolRefs:         ToolRefsFromContext(ctx),
			ToolRefsSet:      ToolRefsSetFromContext(ctx),
			requestContext:   requestContextFromContext(ctx),
		})
	})
	if result == nil {
		return operationResult(http.StatusInternalServerError, nilResultMessage), nil
	}
	return result, nil
}

// ExecuteStream dispatches a streaming operation invocation to its streaming
// handler. It returns a StreamReader that yields InvokeFrame frames. If the
// operation is not registered as streaming, it returns an error.
func (r *Router[P]) ExecuteStream(ctx context.Context, provider *P, operation string, params map[string]any, token string) (StreamReader, error) {
	if r == nil {
		return errStreamReader(http.StatusInternalServerError, streamingRouterNilMessage), nil
	}
	handler, ok := r.streamHandlers[operation]
	if !ok {
		return errStreamReader(http.StatusNotFound, unknownOperationMessage), nil
	}
	reader := protectedStream(operation, func() (StreamReader, error) {
		return handler(ctx, provider, params, Request{
			Token:            token,
			ConnectionParams: ConnectionParams(ctx),
			Subject:          SubjectFromContext(ctx),
			AgentSubject:     AgentSubjectFromContext(ctx),
			Credential:       CredentialFromContext(ctx),
			Access:           AccessFromContext(ctx),
			Host:             HostContextFromContext(ctx),
			IdempotencyKey:   IdempotencyKeyFromContext(ctx),
			ToolRefs:         ToolRefsFromContext(ctx),
			ToolRefsSet:      ToolRefsSetFromContext(ctx),
			requestContext:   requestContextFromContext(ctx),
		})
	})
	return reader, nil
}

// errStreamReader returns a StreamReader that yields a single metadata frame
// carrying the error status and then ends.
func errStreamReader(status int, message string) StreamReader {
	return &errorStreamReader{status: status, message: message}
}

type errorStreamReader struct {
	status   int
	message  string
	sentMeta bool
	sentData bool
}

func (e *errorStreamReader) Recv() (*InvokeFrame, error) {
	if e.sentData {
		return nil, io.EOF
	}
	if !e.sentMeta {
		e.sentMeta = true
		return &InvokeFrame{
			Metadata: &InvokeMetadata{
				Status:    e.status,
				Headers:   http.Header{"Content-Type": []string{"application/json"}},
				MediaType: "application/json",
			},
		}, nil
	}
	e.sentData = true
	body, _ := json.Marshal(map[string]string{"error": e.message})
	return &InvokeFrame{Data: body}, nil
}

// protectedStream wraps a streaming handler call with panic recovery. A
// recovered panic or handler error is surfaced as an error StreamReader (a
// metadata frame carrying the error's HTTP status followed by a JSON error
// body), so callers never observe a nil reader. operationError statuses are
// preserved, matching unary dispatch via operationResultFromError.
func protectedStream(operation string, fn func() (StreamReader, error)) (reader StreamReader) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = recoveredOperationResult(operation, recovered)
			reader = errStreamReader(http.StatusInternalServerError, internalErrorMessage)
		}
	}()
	reader, err := fn()
	if err != nil {
		status := http.StatusInternalServerError
		message := internalErrorMessage
		var opErr *operationError
		if errors.As(err, &opErr) {
			if opErr.status != 0 {
				status = opErr.status
			}
			message = opErr.message
			if message == "" {
				message = opErr.Error()
			}
		}
		return errStreamReader(status, message)
	}
	if reader == nil {
		return errStreamReader(http.StatusInternalServerError, nilResultMessage)
	}
	return reader
}

var _ StreamReader = (*errorStreamReader)(nil)

const streamingRouterNilMessage = "router is nil"

func catalogOperationFor[In any, Out any](op Operation[In, Out]) (*CatalogOperation, error) {
	id := strings.TrimSpace(op.ID)
	if id == "" {
		return nil, fmt.Errorf("operation id is required")
	}
	params, err := catalogParametersFor[In]()
	if err != nil {
		return nil, fmt.Errorf("operation %q: %w", id, err)
	}
	catOp := &CatalogOperation{
		Id:           id,
		Method:       normalizeMethod(op.Method),
		Title:        strings.TrimSpace(op.Title),
		Description:  strings.TrimSpace(op.Description),
		AllowedRoles: append([]string(nil), op.AllowedRoles...),
		Parameters:   params,
		Tags:         append([]string(nil), op.Tags...),
		ReadOnly:     op.ReadOnly,
	}
	if op.Visible != nil {
		catOp.Visible = op.Visible
	}
	return catOp, nil
}

func catalogParametersFor[In any]() ([]*CatalogParameter, error) {
	t := underlyingType(reflect.TypeFor[In]())
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input type %s must be a struct", t)
	}
	if t.NumField() == 0 {
		return nil, nil
	}

	params := make([]*CatalogParameter, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous {
			return nil, fmt.Errorf("field %s: embedded fields are not supported", field.Name)
		}
		if field.PkgPath != "" {
			continue
		}
		name, omitempty, include := jsonField(field)
		if !include {
			continue
		}
		paramType, err := catalogParameterType(field.Type)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		required, err := fieldRequired(field, omitempty)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		}
		param := &CatalogParameter{
			Name:        name,
			Type:        paramType,
			Description: fieldDescription(field),
			Required:    required,
		}
		if def, ok, err := fieldDefault(field); err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Name, err)
		} else if ok {
			param.Default = def
			param.HasDefault = true
		}
		params = append(params, param)
	}
	return params, nil
}

func decodeParams(raw map[string]any, dst any) error {
	if raw == nil {
		raw = map[string]any{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

func normalizeMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return http.MethodPost
	}
	return method
}

func jsonResponseHeaders(headers http.Header) http.Header {
	out := headers.Clone()
	if out == nil {
		out = http.Header{}
	}
	if !hasHeader(out, "Content-Type") {
		out.Set("Content-Type", "application/json")
	}
	return out
}

func hasHeader(headers http.Header, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func jsonField(field reflect.StructField) (name string, omitempty, include bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}
	if tag == "" {
		return lowerCamel(field.Name), false, true
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = lowerCamel(field.Name)
	}
	for _, option := range parts[1:] {
		if option == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty, true
}

func lowerCamel(name string) string {
	if name == "" {
		return ""
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func fieldDescription(field reflect.StructField) string {
	if desc := strings.TrimSpace(field.Tag.Get("doc")); desc != "" {
		return desc
	}
	return strings.TrimSpace(field.Tag.Get("description"))
}

func fieldRequired(field reflect.StructField, omitempty bool) (bool, error) {
	if tag := strings.TrimSpace(field.Tag.Get("required")); tag != "" {
		required, err := strconv.ParseBool(tag)
		if err != nil {
			return false, fmt.Errorf("parse required tag %q: %w", tag, err)
		}
		return required, nil
	}
	return !omitempty && !isOptionalType(field.Type), nil
}

func fieldDefault(field reflect.StructField) (any, bool, error) {
	tag := strings.TrimSpace(field.Tag.Get("default"))
	if tag == "" {
		return nil, false, nil
	}
	return parseDefaultValue(underlyingType(field.Type), tag)
}

func parseDefaultValue(t reflect.Type, value string) (any, bool, error) {
	switch t.Kind() {
	case reflect.String:
		return value, true, nil
	case reflect.Bool:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return nil, false, fmt.Errorf("parse bool default %q: %w", value, err)
		}
		return v, true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parse integer default %q: %w", value, err)
		}
		return v, true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parse unsigned integer default %q: %w", value, err)
		}
		return v, true, nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, false, fmt.Errorf("parse number default %q: %w", value, err)
		}
		return v, true, nil
	default:
		return nil, false, fmt.Errorf("default tags are only supported on scalar fields, got %s", t)
	}
}

func catalogParameterType(t reflect.Type) (string, error) {
	t = underlyingType(t)
	switch t.Kind() {
	case reflect.String:
		return "string", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer", nil
	case reflect.Float32, reflect.Float64:
		return "number", nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string", nil
		}
		return "array", nil
	case reflect.Map, reflect.Interface:
		return "object", nil
	case reflect.Struct:
		if t.PkgPath() == "time" && t.Name() == "Time" {
			return "string", nil
		}
		return "object", nil
	default:
		return "", fmt.Errorf("unsupported field type %s", t)
	}
}

func underlyingType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func isOptionalType(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		return true
	}
	switch t.Kind() {
	case reflect.Interface, reflect.Map, reflect.Slice:
		return true
	default:
		return false
	}
}
