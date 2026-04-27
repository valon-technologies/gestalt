package providerdev

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/core"
	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/internal/principal"
	"github.com/valon-technologies/gestalt/server/internal/providerhost"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	DefaultPollTimeout        = 30 * time.Second
	DefaultSessionIdleTimeout = 2 * time.Minute
	DefaultCallIdleTimeout    = 30 * time.Minute

	PathSessions = "/api/v1/provider-dev/sessions"
)

type RuntimeEnv struct {
	Env          map[string]string
	AllowedHosts []string
	Cleanup      func()
}

type RuntimeEnvBuilder func(sessionID string) (RuntimeEnv, error)

type Target struct {
	Name       string
	Spec       providerhost.StaticProviderSpec
	Config     map[string]any
	RuntimeEnv RuntimeEnvBuilder
}

type Manager struct {
	mu         sync.RWMutex
	targets    map[string]Target
	sessions   map[string]*Session
	ownerIndex map[string][]string
}

type CreateSessionRequest struct {
	Providers []AttachProvider `json:"providers"`
}

type AttachProvider struct {
	Name   string                          `json:"name"`
	Spec   providerhost.StaticProviderSpec `json:"spec"`
	Config map[string]any                  `json:"config,omitempty"`
}

type CreateSessionResponse struct {
	ID        string                  `json:"id"`
	Providers []CreateSessionProvider `json:"providers"`
}

type CreateSessionProvider struct {
	Name         string            `json:"name"`
	Env          map[string]string `json:"env,omitempty"`
	AllowedHosts []string          `json:"allowedHosts,omitempty"`
}

type PollResponse struct {
	CallID   string `json:"callId"`
	Provider string `json:"provider"`
	Method   string `json:"method"`
	Request  string `json:"request"`
}

type CompleteCallRequest struct {
	Response string    `json:"response,omitempty"`
	Error    *RPCError `json:"error,omitempty"`
}

type RPCError struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

type Session struct {
	id      string
	owner   string
	targets map[string]*attachedTarget

	mu       sync.Mutex
	calls    chan *rpcCall
	pending  map[string]*rpcCall
	done     chan struct{}
	lastSeen time.Time
	closeErr error
	closed   bool
}

type attachedTarget struct {
	target     Target
	env        RuntimeEnv
	providerMu sync.Mutex
	provider   core.Provider
	closed     bool
}

type rpcCall struct {
	id          string
	provider    string
	method      string
	request     []byte
	deliveredAt time.Time
	response    chan rpcResponse
}

type rpcResponse struct {
	payload []byte
	err     error
}

func NewManager(targets []Target) (*Manager, error) {
	m := &Manager{
		targets:    make(map[string]Target, len(targets)),
		sessions:   map[string]*Session{},
		ownerIndex: map[string][]string{},
	}
	for i := range targets {
		target := targets[i]
		name := strings.TrimSpace(target.Name)
		if name == "" {
			return nil, fmt.Errorf("provider dev target name is required")
		}
		target.Name = name
		m.targets[name] = target
	}
	return m, nil
}

func (m *Manager) HasTargets() bool {
	return m != nil && len(m.targets) != 0
}

func (m *Manager) CreateSession(ctx context.Context, p *principal.Principal, req CreateSessionRequest) (*CreateSessionResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	owner := principalSubjectID(p)
	if owner == "" {
		return nil, status.Error(codes.Unauthenticated, "provider dev requires an authenticated principal")
	}
	if m == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev is not configured")
	}

	requestedProviders, err := normalizeAttachProviders(req.Providers)
	if err != nil {
		return nil, err
	}
	if len(requestedProviders) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one provider is required")
	}

	m.mu.RLock()
	targets := make([]Target, 0, len(requestedProviders))
	for i := range requestedProviders {
		requested := requestedProviders[i]
		remoteTarget, ok := m.targets[requested.Name]
		if !ok {
			m.mu.RUnlock()
			return nil, status.Errorf(codes.NotFound, "provider %q is not configured on this server", requested.Name)
		}
		target := remoteTarget
		target.Spec = buildAttachSpec(remoteTarget.Spec, requested.Spec)
		target.Config = requested.Config
		targets = append(targets, target)
	}
	m.mu.RUnlock()

	sessionID, err := randomID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create provider dev session: %v", err)
	}
	session := &Session{
		id:       sessionID,
		owner:    owner,
		targets:  make(map[string]*attachedTarget, len(targets)),
		calls:    make(chan *rpcCall, 128),
		pending:  map[string]*rpcCall{},
		done:     make(chan struct{}),
		lastSeen: time.Now(),
	}
	resp := &CreateSessionResponse{
		ID:        sessionID,
		Providers: make([]CreateSessionProvider, 0, len(targets)),
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = session.Close()
		}
	}()

	for i := range targets {
		target := targets[i]
		runtimeEnv := RuntimeEnv{}
		if target.RuntimeEnv != nil {
			runtimeEnv, err = target.RuntimeEnv(sessionID)
			if err != nil {
				return nil, status.Errorf(codes.FailedPrecondition, "prepare provider %q runtime env: %v", target.Name, err)
			}
		}
		session.targets[target.Name] = &attachedTarget{
			target: target,
			env:    runtimeEnv,
		}
		resp.Providers = append(resp.Providers, CreateSessionProvider{
			Name:         target.Name,
			Env:          cloneStringMap(runtimeEnv.Env),
			AllowedHosts: slices.Clone(runtimeEnv.AllowedHosts),
		})
	}

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.ownerIndex[owner] = append(m.ownerIndex[owner], sessionID)
	m.mu.Unlock()
	cleanupOnError = false

	return resp, nil
}

func (m *Manager) PollSession(ctx context.Context, p *principal.Principal, sessionID string) (*PollResponse, bool, error) {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return nil, false, err
	}
	session.touch()

	select {
	case call := <-session.calls:
		session.mu.Lock()
		if session.closed {
			session.mu.Unlock()
			return nil, false, status.Error(codes.NotFound, "provider dev session is closed")
		}
		call.deliveredAt = time.Now()
		session.pending[call.id] = call
		session.lastSeen = call.deliveredAt
		session.mu.Unlock()
		return &PollResponse{
			CallID:   call.id,
			Provider: call.provider,
			Method:   call.method,
			Request:  hex.EncodeToString(call.request),
		}, true, nil
	case <-session.done:
		return nil, false, status.Error(codes.NotFound, "provider dev session is closed")
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, false, nil
		}
		return nil, false, ctx.Err()
	}
}

func (m *Manager) CompleteCall(p *principal.Principal, sessionID, callID string, req CompleteCallRequest) error {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return err
	}
	session.touch()
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return status.Error(codes.InvalidArgument, "call id is required")
	}

	session.mu.Lock()
	call, ok := session.pending[callID]
	if ok {
		delete(session.pending, callID)
	}
	session.mu.Unlock()
	if !ok {
		return status.Errorf(codes.NotFound, "provider dev call %q not found", callID)
	}

	resp := rpcResponse{}
	if req.Error != nil {
		resp.err = status.Error(codes.Code(req.Error.Code), req.Error.Message)
	} else if req.Response != "" {
		payload, err := hex.DecodeString(req.Response)
		if err != nil {
			resp.err = status.Errorf(codes.InvalidArgument, "decode provider dev response: %v", err)
		} else {
			resp.payload = payload
		}
	}

	select {
	case call.response <- resp:
		session.touch()
		return nil
	case <-session.done:
		return status.Error(codes.NotFound, "provider dev session is closed")
	default:
		return status.Error(codes.DeadlineExceeded, "provider dev caller is no longer waiting")
	}
}

func (m *Manager) CloseSession(p *principal.Principal, sessionID string) error {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return err
	}
	if err := session.Close(); err != nil {
		return status.Errorf(codes.Internal, "close provider dev session: %v", err)
	}
	m.removeSession(sessionID, session.owner)
	return nil
}

func (m *Manager) ResolveProviderOverride(ctx context.Context, p *principal.Principal, providerName string) (core.Provider, bool, error) {
	if m == nil {
		return nil, false, nil
	}
	m.closeIdleSessions(time.Now())
	owner := principalSubjectID(p)
	if owner == "" {
		return nil, false, nil
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return nil, false, nil
	}

	m.mu.RLock()
	sessionIDs := append([]string(nil), m.ownerIndex[owner]...)
	sessions := make(map[string]*Session, len(sessionIDs))
	for _, id := range sessionIDs {
		sessions[id] = m.sessions[id]
	}
	m.mu.RUnlock()

	for i := len(sessionIDs) - 1; i >= 0; i-- {
		session := sessions[sessionIDs[i]]
		if session == nil || session.isClosed() {
			continue
		}
		target := session.targets[providerName]
		if target == nil {
			continue
		}
		prov, err := target.providerForSession(ctx, session, providerName)
		if err != nil {
			return nil, false, err
		}
		return prov, true, nil
	}
	return nil, false, nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = map[string]*Session{}
	m.ownerIndex = map[string][]string{}
	m.mu.Unlock()

	var errs []error
	for _, session := range sessions {
		errs = append(errs, session.Close())
	}
	return errors.Join(errs...)
}

func (m *Manager) closeIdleSessions(now time.Time) {
	if m == nil {
		return
	}
	m.mu.RLock()
	var expired []*Session
	for _, session := range m.sessions {
		if session.isIdleExpired(now) {
			expired = append(expired, session)
		}
	}
	m.mu.RUnlock()
	for _, session := range expired {
		_ = session.Close()
		m.removeSession(session.id, session.owner)
	}
}

func (m *Manager) sessionForPrincipal(p *principal.Principal, sessionID string) (*Session, error) {
	owner := principalSubjectID(p)
	if owner == "" {
		return nil, status.Error(codes.Unauthenticated, "provider dev requires an authenticated principal")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session id is required")
	}
	if m == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev is not configured")
	}

	m.mu.RLock()
	session := m.sessions[sessionID]
	m.mu.RUnlock()
	if session == nil || session.isClosed() {
		return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", sessionID)
	}
	if session.owner != owner {
		return nil, status.Error(codes.PermissionDenied, "provider dev session belongs to another principal")
	}
	return session, nil
}

func (m *Manager) removeSession(sessionID, owner string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	ids := m.ownerIndex[owner]
	for i, id := range ids {
		if id != sessionID {
			continue
		}
		ids = append(ids[:i], ids[i+1:]...)
		break
	}
	if len(ids) == 0 {
		delete(m.ownerIndex, owner)
		return
	}
	m.ownerIndex[owner] = ids
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.closed = true
	close(s.done)
	pending := make([]*rpcCall, 0, len(s.pending))
	for id, call := range s.pending {
		delete(s.pending, id)
		pending = append(pending, call)
	}
	targets := make([]*attachedTarget, 0, len(s.targets))
	for _, target := range s.targets {
		targets = append(targets, target)
	}
	s.mu.Unlock()

	for _, call := range pending {
		select {
		case call.response <- rpcResponse{err: status.Error(codes.Unavailable, "provider dev session closed")}:
		default:
		}
	}

	var errs []error
	for _, target := range targets {
		errs = append(errs, target.close())
	}
	s.mu.Lock()
	s.closeErr = errors.Join(errs...)
	err := s.closeErr
	s.mu.Unlock()
	return err
}

func (s *Session) isClosed() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Session) touch() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.lastSeen = time.Now()
	}
}

func (s *Session) isIdleExpired(now time.Time) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return true
	}
	for _, call := range s.pending {
		if call == nil {
			continue
		}
		if call.deliveredAt.IsZero() || now.Sub(call.deliveredAt) <= DefaultCallIdleTimeout {
			return false
		}
	}
	return !s.lastSeen.IsZero() && now.Sub(s.lastSeen) > DefaultSessionIdleTimeout
}

func (s *Session) invoke(ctx context.Context, providerName, method string, req gproto.Message, resp gproto.Message) error {
	if s == nil {
		return status.Error(codes.Unavailable, "provider dev session is unavailable")
	}
	payload, err := gproto.Marshal(req)
	if err != nil {
		return status.Errorf(codes.Internal, "marshal provider dev request: %v", err)
	}
	callID, err := randomID()
	if err != nil {
		return status.Errorf(codes.Internal, "create provider dev call: %v", err)
	}
	call := &rpcCall{
		id:       callID,
		provider: providerName,
		method:   method,
		request:  payload,
		response: make(chan rpcResponse, 1),
	}
	s.touch()

	select {
	case s.calls <- call:
		s.touch()
	case <-s.done:
		return status.Error(codes.Unavailable, "provider dev session is closed")
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case result := <-call.response:
		s.touch()
		if result.err != nil {
			return result.err
		}
		if resp == nil {
			return nil
		}
		if err := gproto.Unmarshal(result.payload, resp); err != nil {
			return status.Errorf(codes.Internal, "unmarshal provider dev response: %v", err)
		}
		return nil
	case <-s.done:
		return status.Error(codes.Unavailable, "provider dev session is closed")
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, callID)
		if !s.closed {
			s.lastSeen = time.Now()
		}
		s.mu.Unlock()
		return ctx.Err()
	}
}

func (t *attachedTarget) providerForSession(ctx context.Context, session *Session, providerName string) (core.Provider, error) {
	t.providerMu.Lock()
	defer t.providerMu.Unlock()
	if t.closed {
		return nil, status.Error(codes.Unavailable, "provider dev target is closed")
	}
	if t.provider != nil {
		return t.provider, nil
	}

	client := &sessionProviderClient{session: session, provider: providerName}
	prov, err := providerhost.NewRemoteProvider(ctx, client, t.target.Spec, t.target.Config)
	if err != nil {
		return nil, err
	}
	t.provider = &attachProvider{Provider: prov, policyCatalog: t.target.Spec.Catalog}
	return t.provider, nil
}

type attachProvider struct {
	core.Provider
	policyCatalog *catalog.Catalog
}

func (p *attachProvider) Catalog() *catalog.Catalog {
	if p == nil || p.Provider == nil {
		return nil
	}
	return buildAttachCatalog(p.policyCatalog, p.Provider.Catalog())
}

func (p *attachProvider) SupportsSessionCatalog() bool {
	return p != nil && p.Provider != nil && core.SupportsSessionCatalog(p.Provider)
}

func (p *attachProvider) CatalogForRequest(ctx context.Context, token string) (*catalog.Catalog, error) {
	if p == nil || p.Provider == nil {
		return nil, core.ErrSessionCatalogUnsupported
	}
	scp, ok := p.Provider.(core.SessionCatalogProvider)
	if !ok {
		return nil, core.ErrSessionCatalogUnsupported
	}
	cat, err := scp.CatalogForRequest(ctx, token)
	if err != nil {
		return nil, err
	}
	if cat == nil {
		return nil, nil
	}
	return buildAttachCatalog(p.policyCatalog, cat), nil
}

func (p *attachProvider) SupportsPostConnect() bool {
	return p != nil && p.Provider != nil && core.SupportsPostConnect(p.Provider)
}

func (p *attachProvider) PostConnect(ctx context.Context, token *core.IntegrationToken) (map[string]string, error) {
	if p == nil || p.Provider == nil {
		return nil, core.ErrPostConnectUnsupported
	}
	pcp, ok := p.Provider.(core.PostConnectCapable)
	if !ok {
		return nil, core.ErrPostConnectUnsupported
	}
	return pcp.PostConnect(ctx, token)
}

func (p *attachProvider) Close() error {
	if p == nil || p.Provider == nil {
		return nil
	}
	if c, ok := p.Provider.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

func (t *attachedTarget) close() error {
	if t == nil {
		return nil
	}
	t.providerMu.Lock()
	t.closed = true
	prov := t.provider
	t.provider = nil
	t.providerMu.Unlock()

	var errs []error
	if c, ok := prov.(interface{ Close() error }); ok {
		errs = append(errs, c.Close())
	}
	if t.env.Cleanup != nil {
		t.env.Cleanup()
	}
	return errors.Join(errs...)
}

type sessionProviderClient struct {
	session  *Session
	provider string
}

func (c *sessionProviderClient) GetMetadata(ctx context.Context, req *emptypb.Empty, _ ...grpc.CallOption) (*proto.ProviderMetadata, error) {
	resp := &proto.ProviderMetadata{}
	err := c.session.invoke(ctx, c.provider, "GetMetadata", req, resp)
	return resp, err
}

func (c *sessionProviderClient) StartProvider(ctx context.Context, req *proto.StartProviderRequest, _ ...grpc.CallOption) (*proto.StartProviderResponse, error) {
	resp := &proto.StartProviderResponse{}
	err := c.session.invoke(ctx, c.provider, "StartProvider", req, resp)
	return resp, err
}

func (c *sessionProviderClient) Execute(ctx context.Context, req *proto.ExecuteRequest, _ ...grpc.CallOption) (*proto.OperationResult, error) {
	resp := &proto.OperationResult{}
	err := c.session.invoke(ctx, c.provider, "Execute", req, resp)
	return resp, err
}

func (c *sessionProviderClient) ResolveHTTPSubject(ctx context.Context, req *proto.ResolveHTTPSubjectRequest, _ ...grpc.CallOption) (*proto.ResolveHTTPSubjectResponse, error) {
	resp := &proto.ResolveHTTPSubjectResponse{}
	err := c.session.invoke(ctx, c.provider, "ResolveHTTPSubject", req, resp)
	return resp, err
}

func (c *sessionProviderClient) GetSessionCatalog(ctx context.Context, req *proto.GetSessionCatalogRequest, _ ...grpc.CallOption) (*proto.GetSessionCatalogResponse, error) {
	resp := &proto.GetSessionCatalogResponse{}
	err := c.session.invoke(ctx, c.provider, "GetSessionCatalog", req, resp)
	return resp, err
}

func (c *sessionProviderClient) PostConnect(ctx context.Context, req *proto.PostConnectRequest, _ ...grpc.CallOption) (*proto.PostConnectResponse, error) {
	resp := &proto.PostConnectResponse{}
	err := c.session.invoke(ctx, c.provider, "PostConnect", req, resp)
	return resp, err
}

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func (c Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	var out CreateSessionResponse
	if err := c.doJSON(ctx, http.MethodPost, PathSessions, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c Client) Poll(ctx context.Context, sessionID string) (*PollResponse, bool, error) {
	path := PathSessions + "/" + url.PathEscape(sessionID) + "/poll"
	var out PollResponse
	statusCode, err := c.doJSONStatus(ctx, http.MethodGet, path, nil, &out)
	if err != nil {
		return nil, false, err
	}
	if statusCode == http.StatusNoContent {
		return nil, false, nil
	}
	return &out, true, nil
}

func (c Client) Complete(ctx context.Context, sessionID, callID string, req CompleteCallRequest) error {
	path := PathSessions + "/" + url.PathEscape(sessionID) + "/calls/" + url.PathEscape(callID)
	return c.doJSON(ctx, http.MethodPost, path, req, nil)
}

func (c Client) CloseSession(ctx context.Context, sessionID string) error {
	path := PathSessions + "/" + url.PathEscape(sessionID)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (c Client) RunDispatcher(ctx context.Context, sessionID string, providers map[string]proto.IntegrationProviderClient) error {
	for {
		call, ok, err := c.Poll(ctx, sessionID)
		if err != nil {
			if dispatcherContextDone(ctx) {
				return nil
			}
			return err
		}
		if !ok {
			continue
		}
		req := c.dispatchCall(ctx, call, providers)
		if err := c.Complete(ctx, sessionID, call.CallID, req); err != nil {
			if dispatcherContextDone(ctx) {
				return nil
			}
			return err
		}
	}
}

func dispatcherContextDone(ctx context.Context) bool {
	return ctx.Err() != nil
}

func (c Client) dispatchCall(ctx context.Context, call *PollResponse, providers map[string]proto.IntegrationProviderClient) CompleteCallRequest {
	client := providers[strings.TrimSpace(call.Provider)]
	if client == nil {
		return CompleteCallRequest{Error: encodeRPCError(status.Errorf(codes.NotFound, "local provider %q is not running", call.Provider))}
	}
	payload, err := hex.DecodeString(call.Request)
	if err != nil {
		return CompleteCallRequest{Error: encodeRPCError(status.Errorf(codes.InvalidArgument, "decode request: %v", err))}
	}
	resp, err := dispatchProviderRPC(ctx, client, call.Method, payload)
	if err != nil {
		return CompleteCallRequest{Error: encodeRPCError(err)}
	}
	return CompleteCallRequest{Response: hex.EncodeToString(resp)}
}

func dispatchProviderRPC(ctx context.Context, client proto.IntegrationProviderClient, method string, payload []byte) ([]byte, error) {
	switch method {
	case "GetMetadata":
		req := &emptypb.Empty{}
		if err := gproto.Unmarshal(payload, req); err != nil {
			return nil, err
		}
		resp, err := client.GetMetadata(ctx, req)
		if err != nil {
			return nil, err
		}
		return gproto.Marshal(resp)
	case "StartProvider":
		req := &proto.StartProviderRequest{}
		if err := gproto.Unmarshal(payload, req); err != nil {
			return nil, err
		}
		resp, err := client.StartProvider(ctx, req)
		if err != nil {
			return nil, err
		}
		return gproto.Marshal(resp)
	case "Execute":
		req := &proto.ExecuteRequest{}
		if err := gproto.Unmarshal(payload, req); err != nil {
			return nil, err
		}
		resp, err := client.Execute(ctx, req)
		if err != nil {
			return nil, err
		}
		return gproto.Marshal(resp)
	case "ResolveHTTPSubject":
		req := &proto.ResolveHTTPSubjectRequest{}
		if err := gproto.Unmarshal(payload, req); err != nil {
			return nil, err
		}
		resp, err := client.ResolveHTTPSubject(ctx, req)
		if err != nil {
			return nil, err
		}
		return gproto.Marshal(resp)
	case "GetSessionCatalog":
		req := &proto.GetSessionCatalogRequest{}
		if err := gproto.Unmarshal(payload, req); err != nil {
			return nil, err
		}
		resp, err := client.GetSessionCatalog(ctx, req)
		if err != nil {
			return nil, err
		}
		return gproto.Marshal(resp)
	case "PostConnect":
		req := &proto.PostConnectRequest{}
		if err := gproto.Unmarshal(payload, req); err != nil {
			return nil, err
		}
		resp, err := client.PostConnect(ctx, req)
		if err != nil {
			return nil, err
		}
		return gproto.Marshal(resp)
	default:
		return nil, status.Errorf(codes.Unimplemented, "provider dev method %q is not supported", method)
	}
}

func (c Client) doJSON(ctx context.Context, method, path string, in any, out any) error {
	_, err := c.doJSONStatus(ctx, method, path, in, out)
	return err
}

func (c Client) doJSONStatus(ctx context.Context, method, path string, in any, out any) (int, error) {
	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(payload)
	}
	endpoint, err := c.endpoint(path)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(c.Token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, fmt.Errorf("provider dev remote %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(payload)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (c Client) endpoint(path string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(c.BaseURL))
	if err != nil {
		return "", err
	}
	if base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("remote URL must include scheme and host")
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func encodeRPCError(err error) *RPCError {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if ok {
		return &RPCError{Code: int32(st.Code()), Message: st.Message()}
	}
	return &RPCError{Code: int32(codes.Unknown), Message: err.Error()}
}

func normalizeAttachProviders(values []AttachProvider) ([]AttachProvider, error) {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for i := range values {
		value := values[i]
		name := strings.TrimSpace(value.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if specName := strings.TrimSpace(value.Spec.Name); specName != "" && specName != name {
			return nil, status.Errorf(codes.InvalidArgument, "provider spec name %q must match requested provider name %q", specName, name)
		}
		value.Name = name
		value.Spec.Name = name
		seen[name] = struct{}{}
		out = append(out, name)
	}
	slices.Sort(out)
	requests := make([]AttachProvider, 0, len(out))
	for _, name := range out {
		for i := range values {
			value := values[i]
			if strings.TrimSpace(value.Name) != name {
				continue
			}
			value.Name = name
			value.Spec.Name = name
			requests = append(requests, value)
			break
		}
	}
	return requests, nil
}

func buildAttachSpec(remoteSpec providerhost.StaticProviderSpec, localSpec providerhost.StaticProviderSpec) providerhost.StaticProviderSpec {
	spec := cloneStaticProviderSpec(remoteSpec)
	if spec.Name == "" {
		spec.Name = localSpec.Name
	}
	if localSpec.DisplayName != "" {
		spec.DisplayName = localSpec.DisplayName
	}
	if localSpec.Description != "" {
		spec.Description = localSpec.Description
	}
	if localSpec.IconSVG != "" {
		spec.IconSVG = localSpec.IconSVG
	}
	spec.Catalog = buildAttachCatalog(remoteSpec.Catalog, localSpec.Catalog)
	return spec
}

func buildAttachCatalog(remoteCat, localCat *catalog.Catalog) *catalog.Catalog {
	if localCat == nil {
		if remoteCat == nil {
			return nil
		}
		return remoteCat.Clone()
	}
	out := localCat.Clone()
	remoteOps := map[string]catalog.CatalogOperation{}
	if remoteCat != nil {
		out.BaseURL = remoteCat.BaseURL
		out.AuthStyle = remoteCat.AuthStyle
		out.Headers = cloneStringMap(remoteCat.Headers)
		for i := range remoteCat.Operations {
			op := remoteCat.Operations[i]
			remoteOps[op.ID] = op
		}
	}
	for i := range out.Operations {
		remoteOp, ok := remoteOps[out.Operations[i].ID]
		if !ok {
			out.Operations[i].AllowedRoles = nil
			out.Operations[i].RequiredScopes = nil
			continue
		}
		out.Operations[i].AllowedRoles = slices.Clone(remoteOp.AllowedRoles)
		out.Operations[i].RequiredScopes = slices.Clone(remoteOp.RequiredScopes)
	}
	return out
}

func cloneStaticProviderSpec(spec providerhost.StaticProviderSpec) providerhost.StaticProviderSpec {
	out := spec
	out.AuthTypes = slices.Clone(spec.AuthTypes)
	if spec.ConnectionParams != nil {
		out.ConnectionParams = make(map[string]core.ConnectionParamDef, len(spec.ConnectionParams))
		for key, value := range spec.ConnectionParams {
			out.ConnectionParams[key] = value
		}
	}
	out.CredentialFields = slices.Clone(spec.CredentialFields)
	if spec.Catalog != nil {
		out.Catalog = spec.Catalog.Clone()
	}
	if spec.DiscoveryConfig != nil {
		discovery := *spec.DiscoveryConfig
		out.DiscoveryConfig = &discovery
	}
	return out
}

func principalSubjectID(p *principal.Principal) string {
	if p == nil {
		return ""
	}
	if subjectID := strings.TrimSpace(p.SubjectID); subjectID != "" {
		return subjectID
	}
	if userID := strings.TrimSpace(p.UserID); userID != "" {
		return principal.UserSubjectID(userID)
	}
	return ""
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
