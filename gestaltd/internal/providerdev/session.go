package providerdev

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	DefaultPollTimeout             = 30 * time.Second
	DefaultSessionIdleTimeout      = 2 * time.Minute
	DefaultCallIdleTimeout         = 30 * time.Minute
	DefaultMaxAttachAuthorizations = 1024

	HeaderDispatcherSecret    = "X-Gestalt-Provider-Dev-Dispatcher"
	HeaderAuthorizationSecret = "X-Gestalt-Provider-Dev-Authorization"

	PathAttachments          = "/api/v1/provider-dev/attachments"
	PathAttachAuthorizations = "/api/v1/provider-dev/attach-authorizations"
	PathSessions             = "/api/v1/provider-dev/sessions"
)

type RuntimeEnv struct {
	Env          map[string]string
	AllowedHosts []string
	Cleanup      func()
}

type RuntimeEnvBuilder func(sessionID string) (RuntimeEnv, error)

type Target struct {
	Name       string
	Source     string
	Spec       providerhost.StaticProviderSpec
	Config     map[string]any
	UI         *AttachUI
	UIPath     string
	RuntimeEnv RuntimeEnvBuilder
}

type Manager struct {
	mu             sync.RWMutex
	targets        map[string]Target
	sessions       map[string]*Session
	ownerIndex     map[string][]string
	authorizations map[string]*AttachAuthorization
}

type CreateSessionRequest struct {
	Providers               []AttachProvider `json:"providers"`
	AttachAuthorizationCode string           `json:"attachAuthorizationCode,omitempty"`
}

type AttachProvider struct {
	Name   string                          `json:"name"`
	Source string                          `json:"source,omitempty"`
	Spec   providerhost.StaticProviderSpec `json:"spec"`
	Config *map[string]any                 `json:"config,omitempty"`
	UI     *AttachUI                       `json:"ui,omitempty"`
}

type CreateSessionResponse struct {
	ID               string                  `json:"id,omitempty"`
	AttachID         string                  `json:"attachId,omitempty"`
	DispatcherSecret string                  `json:"dispatcherSecret,omitempty"`
	Providers        []CreateSessionProvider `json:"providers"`
}

type CreateSessionProvider struct {
	Name         string            `json:"name"`
	Env          map[string]string `json:"env,omitempty"`
	AllowedHosts []string          `json:"allowedHosts,omitempty"`
	Source       string            `json:"source,omitempty"`
	UI           bool              `json:"ui,omitempty"`
	UIPath       string            `json:"uiPath,omitempty"`
}

type CreateAttachAuthorizationResponse struct {
	AuthorizationID    string    `json:"authorizationId"`
	ClientSecret       string    `json:"clientSecret"`
	VerificationCode   string    `json:"verificationCode"`
	ApprovalURL        string    `json:"approvalUrl"`
	ExpiresAt          time.Time `json:"expiresAt"`
	PollIntervalMillis int       `json:"pollIntervalMillis"`
}

type AttachAuthorizationInfo struct {
	AuthorizationID string    `json:"authorizationId"`
	Providers       []string  `json:"providers"`
	ExpiresAt       time.Time `json:"expiresAt"`
	Approved        bool      `json:"approved"`
}

type PollAttachAuthorizationResponse struct {
	Approved                bool   `json:"approved"`
	AttachAuthorizationCode string `json:"attachAuthorizationCode,omitempty"`
}

type AttachmentInfo struct {
	AttachID           string                   `json:"attachId"`
	CreatedAt          time.Time                `json:"createdAt"`
	LastSeenAt         time.Time                `json:"lastSeenAt"`
	IdleTimeoutSeconds int                      `json:"idleTimeoutSeconds"`
	Providers          []AttachmentProviderInfo `json:"providers"`
}

type ListAttachmentsResponse struct {
	Attachments []AttachmentInfo `json:"attachments"`
}

type AttachmentProviderInfo struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	UI     bool   `json:"ui,omitempty"`
	UIPath string `json:"uiPath,omitempty"`
}

type AttachUI struct{}

type AttachAuthorization struct {
	id               string
	clientSecretHash string
	verificationHash string
	requestHash      string
	providers        []string
	createdAt        time.Time
	expiresAt        time.Time
	approvedAt       time.Time
	approvedBy       *principal.Principal
	codeHash         string
	code             string
	used             bool
}

type UIAssetRequest struct {
	Method   string      `json:"method,omitempty"`
	Path     string      `json:"path"`
	RawQuery string      `json:"rawQuery,omitempty"`
	Header   http.Header `json:"header,omitempty"`
}

type UIAssetResponse struct {
	Status int         `json:"status"`
	Header http.Header `json:"header,omitempty"`
	Body   string      `json:"body,omitempty"`
}

type PollResponse struct {
	CallID   string `json:"callId"`
	Provider string `json:"provider"`
	Method   string `json:"method"`
	Request  string `json:"request"`
}

type CompleteCallRequest struct {
	Response       string    `json:"response,omitempty"`
	ResponseBase64 string    `json:"responseBase64,omitempty"`
	Error          *RPCError `json:"error,omitempty"`
}

type RPCError struct {
	Code    int32  `json:"code"`
	Message string `json:"message"`
}

type Session struct {
	id                   string
	dispatcherSecretHash string
	owner                string
	targets              map[string]*attachedTarget

	mu        sync.Mutex
	calls     chan *rpcCall
	pending   map[string]*rpcCall
	done      chan struct{}
	closeDone chan struct{}
	createdAt time.Time
	lastSeen  time.Time
	closeErr  error
	closed    bool
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
	canceled    bool
	response    chan rpcResponse
}

type rpcResponse struct {
	payload []byte
	err     error
}

func NewManager(targets []Target) (*Manager, error) {
	m := &Manager{
		targets:        make(map[string]Target, len(targets)),
		sessions:       map[string]*Session{},
		ownerIndex:     map[string][]string{},
		authorizations: map[string]*AttachAuthorization{},
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

	targets, err := m.resolveAttachTargets(requestedProviders)
	if err != nil {
		return nil, err
	}

	sessionID, err := randomID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create provider dev session: %v", err)
	}
	dispatcherSecret, dispatcherSecretHash, err := newDispatcherSecret()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create provider dev dispatcher secret: %v", err)
	}
	now := time.Now()
	session := &Session{
		id:                   sessionID,
		dispatcherSecretHash: dispatcherSecretHash,
		owner:                owner,
		targets:              make(map[string]*attachedTarget, len(targets)),
		calls:                make(chan *rpcCall, 128),
		pending:              map[string]*rpcCall{},
		done:                 make(chan struct{}),
		closeDone:            make(chan struct{}),
		createdAt:            now,
		lastSeen:             now,
	}
	resp := &CreateSessionResponse{
		ID:               sessionID,
		AttachID:         sessionID,
		DispatcherSecret: dispatcherSecret,
		Providers:        make([]CreateSessionProvider, 0, len(targets)),
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
			Source:       target.Source,
			UI:           target.UI != nil,
			UIPath:       target.UIPath,
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
	return session.poll(ctx)
}

func (m *Manager) PollSessionWithDispatcherSecret(ctx context.Context, p *principal.Principal, sessionID, dispatcherSecret string) (*PollResponse, bool, error) {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return nil, false, err
	}
	if err := session.verifyDispatcherSecret(dispatcherSecret); err != nil {
		return nil, false, err
	}
	return session.poll(ctx)
}

func (m *Manager) PollSessionWithDispatcherSecretOnly(ctx context.Context, sessionID, dispatcherSecret string) (*PollResponse, bool, error) {
	session, err := m.sessionForDispatcherSecret(sessionID, dispatcherSecret)
	if err != nil {
		return nil, false, err
	}
	return session.poll(ctx)
}

func (s *Session) poll(ctx context.Context) (*PollResponse, bool, error) {
	s.touch()

	for {
		select {
		case call := <-s.calls:
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				return nil, false, status.Error(codes.NotFound, "provider dev session is closed")
			}
			if call.canceled {
				s.mu.Unlock()
				continue
			}
			call.deliveredAt = time.Now()
			s.pending[call.id] = call
			s.lastSeen = call.deliveredAt
			s.mu.Unlock()
			return &PollResponse{
				CallID:   call.id,
				Provider: call.provider,
				Method:   call.method,
				Request:  hex.EncodeToString(call.request),
			}, true, nil
		case <-s.done:
			return nil, false, status.Error(codes.NotFound, "provider dev session is closed")
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
				return nil, false, nil
			}
			return nil, false, ctx.Err()
		}
	}
}

func (m *Manager) ResolveAttachProviderNames(req CreateSessionRequest) ([]string, error) {
	if m == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev is not configured")
	}
	requestedProviders, err := normalizeAttachProviders(req.Providers)
	if err != nil {
		return nil, err
	}
	targets, err := m.resolveAttachTargets(requestedProviders)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(targets))
	for i := range targets {
		names = append(names, targets[i].Name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	return names, nil
}

func (m *Manager) CreateAttachAuthorization(req CreateSessionRequest, now time.Time) (*AttachAuthorizationInfo, string, string, error) {
	if m == nil {
		return nil, "", "", status.Error(codes.FailedPrecondition, "provider dev is not configured")
	}
	names, err := m.ResolveAttachProviderNames(req)
	if err != nil {
		return nil, "", "", err
	}
	if len(names) == 0 {
		return nil, "", "", status.Error(codes.InvalidArgument, "at least one provider is required")
	}
	requestHash, err := attachAuthorizationRequestHash(req)
	if err != nil {
		return nil, "", "", status.Errorf(codes.Internal, "hash provider dev attach authorization request: %v", err)
	}
	id, err := randomID()
	if err != nil {
		return nil, "", "", status.Errorf(codes.Internal, "create provider dev attach authorization id: %v", err)
	}
	clientSecret, clientSecretHash, err := newAttachAuthorizationSecret()
	if err != nil {
		return nil, "", "", status.Errorf(codes.Internal, "create provider dev attach authorization secret: %v", err)
	}
	verificationCode, verificationHash, err := newAttachAuthorizationVerificationCode()
	if err != nil {
		return nil, "", "", status.Errorf(codes.Internal, "create provider dev attach verification code: %v", err)
	}
	if now.IsZero() {
		now = time.Now()
	}
	auth := &AttachAuthorization{
		id:               id,
		clientSecretHash: clientSecretHash,
		verificationHash: verificationHash,
		requestHash:      requestHash,
		providers:        slices.Clone(names),
		createdAt:        now,
		expiresAt:        now.Add(5 * time.Minute),
	}
	m.mu.Lock()
	m.closeExpiredAuthorizationsLocked(now)
	if len(m.authorizations) >= DefaultMaxAttachAuthorizations {
		m.mu.Unlock()
		return nil, "", "", status.Errorf(codes.ResourceExhausted, "too many pending provider dev attach authorizations; try again later")
	}
	m.authorizations[id] = auth
	m.mu.Unlock()
	info := auth.info()
	return &info, clientSecret, verificationCode, nil
}

func (m *Manager) GetAttachAuthorization(id string) (*AttachAuthorizationInfo, error) {
	auth, err := m.attachAuthorization(id, time.Now())
	if err != nil {
		return nil, err
	}
	info := auth.info()
	return &info, nil
}

func (m *Manager) ApproveAttachAuthorization(id string, p *principal.Principal, verificationCode string) error {
	owner := principalSubjectID(p)
	if owner == "" {
		return status.Error(codes.Unauthenticated, "provider dev attach authorization requires an authenticated principal")
	}
	verificationCode = normalizeAttachAuthorizationVerificationCode(verificationCode)
	if verificationCode == "" {
		return status.Error(codes.InvalidArgument, "provider dev attach verification code is required")
	}
	auth, err := m.attachAuthorization(id, time.Now())
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if auth.used {
		return status.Error(codes.FailedPrecondition, "provider dev attach authorization was already used")
	}
	if subtle.ConstantTimeCompare([]byte(hashAttachAuthorizationSecret(verificationCode)), []byte(auth.verificationHash)) != 1 {
		return status.Error(codes.PermissionDenied, "provider dev attach verification code is invalid")
	}
	if auth.approvedBy != nil || !auth.approvedAt.IsZero() {
		if principalSubjectID(auth.approvedBy) == owner {
			return nil
		}
		return status.Error(codes.FailedPrecondition, "provider dev attach authorization is already approved")
	}
	code, codeHash, err := newAttachAuthorizationCode()
	if err != nil {
		return status.Errorf(codes.Internal, "create provider dev attach authorization code: %v", err)
	}
	auth.approvedAt = time.Now()
	auth.approvedBy = clonePrincipal(p)
	auth.code = code
	auth.codeHash = codeHash
	return nil
}

func (m *Manager) PollAttachAuthorization(id, clientSecret string) (*PollAttachAuthorizationResponse, error) {
	auth, err := m.attachAuthorizationWithClientSecret(id, clientSecret, time.Now())
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return &PollAttachAuthorizationResponse{
		Approved:                !auth.approvedAt.IsZero(),
		AttachAuthorizationCode: auth.code,
	}, nil
}

func (m *Manager) ConsumeAttachAuthorization(id, clientSecret, code string, req CreateSessionRequest) (*principal.Principal, error) {
	auth, err := m.attachAuthorizationWithClientSecret(id, clientSecret, time.Now())
	if err != nil {
		return nil, err
	}
	requestHash, err := attachAuthorizationRequestHash(req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "hash provider dev attach authorization request: %v", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, status.Error(codes.Unauthenticated, "provider dev attach authorization code is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if auth.used {
		return nil, status.Error(codes.FailedPrecondition, "provider dev attach authorization was already used")
	}
	if auth.approvedBy == nil || auth.codeHash == "" {
		return nil, status.Error(codes.FailedPrecondition, "provider dev attach authorization is not approved")
	}
	if subtle.ConstantTimeCompare([]byte(hashAttachAuthorizationSecret(code)), []byte(auth.codeHash)) != 1 {
		return nil, status.Error(codes.PermissionDenied, "provider dev attach authorization code is invalid")
	}
	if subtle.ConstantTimeCompare([]byte(requestHash), []byte(auth.requestHash)) != 1 {
		return nil, status.Error(codes.PermissionDenied, "provider dev attach authorization request does not match")
	}
	auth.used = true
	return clonePrincipal(auth.approvedBy), nil
}

func (m *Manager) resolveAttachTargets(requestedProviders []AttachProvider) ([]Target, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	targets := make([]Target, 0, len(requestedProviders))
	for i := range requestedProviders {
		requested := requestedProviders[i]
		remoteTarget, err := m.resolveAttachTargetLocked(requested)
		if err != nil {
			return nil, err
		}
		target := remoteTarget
		target.Spec = buildAttachSpec(remoteTarget.Spec, requested.Spec)
		if requested.Config != nil {
			target.Config = cloneAnyMap(*requested.Config)
		}
		target.UI = cloneAttachUI(requested.UI)
		targets = append(targets, target)
	}
	return targets, nil
}

func (m *Manager) resolveAttachTargetLocked(requested AttachProvider) (Target, error) {
	name := strings.TrimSpace(requested.Name)
	if name != "" {
		remoteTarget, ok := m.targets[name]
		if !ok {
			return Target{}, status.Errorf(codes.NotFound, "provider %q is not configured on this server", name)
		}
		return remoteTarget, nil
	}

	source := strings.TrimSpace(requested.Source)
	if source == "" {
		return Target{}, status.Error(codes.InvalidArgument, "provider name or source is required")
	}
	var matches []Target
	for name := range m.targets {
		target := m.targets[name]
		if strings.TrimSpace(target.Source) == source {
			matches = append(matches, target)
		}
	}
	if len(matches) == 0 {
		return Target{}, status.Errorf(codes.NotFound, "provider source %q is not configured on this server", source)
	}
	if len(matches) > 1 {
		names := make([]string, 0, len(matches))
		for i := range matches {
			names = append(names, matches[i].Name)
		}
		slices.Sort(names)
		return Target{}, status.Errorf(codes.InvalidArgument, "provider source %q matches multiple providers (%s); pass --name to choose one", source, strings.Join(names, ", "))
	}
	return matches[0], nil
}

func (m *Manager) CompleteCall(p *principal.Principal, sessionID, callID string, req CompleteCallRequest) error {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return err
	}
	return session.completeCall(callID, req)
}

func (m *Manager) CompleteCallWithDispatcherSecret(p *principal.Principal, sessionID, callID, dispatcherSecret string, req CompleteCallRequest) error {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return err
	}
	if err := session.verifyDispatcherSecret(dispatcherSecret); err != nil {
		return err
	}
	return session.completeCall(callID, req)
}

func (m *Manager) CompleteCallWithDispatcherSecretOnly(sessionID, callID, dispatcherSecret string, req CompleteCallRequest) error {
	session, err := m.sessionForDispatcherSecret(sessionID, dispatcherSecret)
	if err != nil {
		return err
	}
	return session.completeCall(callID, req)
}

func (m *Manager) VerifyDispatcherSecret(p *principal.Principal, sessionID, dispatcherSecret string) error {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return err
	}
	return session.verifyDispatcherSecret(dispatcherSecret)
}

func (m *Manager) VerifyDispatcherSecretOnly(sessionID, dispatcherSecret string) error {
	_, err := m.sessionForDispatcherSecret(sessionID, dispatcherSecret)
	return err
}

func (m *Manager) CloseSessionWithDispatcherSecret(sessionID, dispatcherSecret string) error {
	session, err := m.sessionForDispatcherSecret(sessionID, dispatcherSecret)
	if err != nil {
		return err
	}
	if err := session.Close(); err != nil {
		return status.Errorf(codes.Internal, "close provider dev session: %v", err)
	}
	m.removeSession(sessionID, session.owner)
	return nil
}

func (s *Session) completeCall(callID string, req CompleteCallRequest) error {
	s.touch()
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return status.Error(codes.InvalidArgument, "call id is required")
	}

	s.mu.Lock()
	call, ok := s.pending[callID]
	if ok {
		delete(s.pending, callID)
	}
	s.mu.Unlock()
	if !ok {
		return status.Errorf(codes.NotFound, "provider dev call %q not found", callID)
	}

	resp := rpcResponse{}
	switch {
	case req.Error != nil:
		resp.err = status.Error(codes.Code(req.Error.Code), req.Error.Message)
		if resp.err == nil {
			resp.err = status.Error(codes.InvalidArgument, "provider dev error code must be non-OK")
		}
	case req.ResponseBase64 != "":
		payload, err := base64.StdEncoding.DecodeString(req.ResponseBase64)
		if err != nil {
			resp.err = status.Errorf(codes.InvalidArgument, "decode provider dev response: %v", err)
		} else {
			resp.payload = payload
		}
	case req.Response != "":
		payload, err := hex.DecodeString(req.Response)
		if err != nil {
			resp.err = status.Errorf(codes.InvalidArgument, "decode provider dev response: %v", err)
		} else {
			resp.payload = payload
		}
	}

	select {
	case call.response <- resp:
		s.touch()
		return nil
	case <-s.done:
		return status.Error(codes.NotFound, "provider dev session is closed")
	default:
		return status.Error(codes.DeadlineExceeded, "provider dev caller is no longer waiting")
	}
}

func (m *Manager) ListSessions(p *principal.Principal) ([]AttachmentInfo, error) {
	owner := principalSubjectID(p)
	if owner == "" {
		return nil, status.Error(codes.Unauthenticated, "provider dev requires an authenticated principal")
	}
	if m == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev is not configured")
	}
	m.closeIdleSessions(time.Now())
	m.mu.RLock()
	sessionIDs := append([]string(nil), m.ownerIndex[owner]...)
	sessions := make([]*Session, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		if session := m.sessions[id]; session != nil {
			sessions = append(sessions, session)
		}
	}
	m.mu.RUnlock()
	out := make([]AttachmentInfo, 0, len(sessions))
	for _, session := range sessions {
		if session == nil || session.isClosed() {
			continue
		}
		out = append(out, session.attachmentInfo())
	}
	return out, nil
}

func (m *Manager) GetSession(p *principal.Principal, sessionID string) (*AttachmentInfo, error) {
	session, err := m.sessionForPrincipal(p, sessionID)
	if err != nil {
		return nil, err
	}
	info := session.attachmentInfo()
	return &info, nil
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

func (m *Manager) ServeUIAsset(ctx context.Context, p *principal.Principal, providerName string, req UIAssetRequest) (*UIAssetResponse, bool, error) {
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
		if target == nil || target.target.UI == nil {
			continue
		}
		resp, err := session.serveUIAsset(ctx, providerName, req)
		if err != nil {
			return nil, true, err
		}
		return resp, true, nil
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
	m.authorizations = map[string]*AttachAuthorization{}
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

func (m *Manager) attachAuthorization(id string, now time.Time) (*AttachAuthorization, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "provider dev attach authorization id is required")
	}
	if m == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev is not configured")
	}
	if now.IsZero() {
		now = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeExpiredAuthorizationsLocked(now)
	auth := m.authorizations[id]
	if auth == nil {
		return nil, status.Errorf(codes.NotFound, "provider dev attach authorization %q not found", id)
	}
	if !auth.expiresAt.IsZero() && now.After(auth.expiresAt) {
		delete(m.authorizations, id)
		return nil, status.Error(codes.DeadlineExceeded, "provider dev attach authorization expired")
	}
	return auth, nil
}

func (m *Manager) attachAuthorizationWithClientSecret(id, clientSecret string, now time.Time) (*AttachAuthorization, error) {
	auth, err := m.attachAuthorization(id, now)
	if err != nil {
		return nil, err
	}
	clientSecret = strings.TrimSpace(clientSecret)
	if clientSecret == "" {
		return nil, status.Error(codes.Unauthenticated, "provider dev attach authorization secret is required")
	}
	if subtle.ConstantTimeCompare([]byte(hashAttachAuthorizationSecret(clientSecret)), []byte(auth.clientSecretHash)) != 1 {
		return nil, status.Error(codes.PermissionDenied, "provider dev attach authorization secret is invalid")
	}
	return auth, nil
}

func (m *Manager) closeExpiredAuthorizationsLocked(now time.Time) {
	for id, auth := range m.authorizations {
		if auth == nil || (!auth.expiresAt.IsZero() && now.After(auth.expiresAt)) || auth.used {
			delete(m.authorizations, id)
		}
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

func (m *Manager) sessionForDispatcherSecret(sessionID, dispatcherSecret string) (*Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session id is required")
	}
	if m == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider dev is not configured")
	}
	m.closeIdleSessions(time.Now())
	m.mu.RLock()
	session := m.sessions[sessionID]
	m.mu.RUnlock()
	if session == nil || session.isClosed() {
		return nil, status.Errorf(codes.NotFound, "provider dev session %q not found", sessionID)
	}
	if err := session.verifyDispatcherSecret(dispatcherSecret); err != nil {
		return nil, err
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
	if s.closeDone == nil {
		s.closeDone = make(chan struct{})
	}
	if s.closed {
		closeDone := s.closeDone
		s.mu.Unlock()
		<-closeDone
		s.mu.Lock()
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.closed = true
	close(s.done)
	closeDone := s.closeDone
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
	close(closeDone)
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

func (s *Session) verifyDispatcherSecret(secret string) error {
	if s == nil {
		return status.Error(codes.NotFound, "provider dev session not found")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" || s.dispatcherSecretHash == "" {
		return status.Error(codes.Unauthenticated, "provider dev dispatcher secret is required")
	}
	if subtle.ConstantTimeCompare([]byte(hashDispatcherSecret(secret)), []byte(s.dispatcherSecretHash)) != 1 {
		return status.Error(codes.PermissionDenied, "provider dev dispatcher secret is invalid")
	}
	return nil
}

func (s *Session) attachmentInfo() AttachmentInfo {
	if s == nil {
		return AttachmentInfo{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	providers := make([]AttachmentProviderInfo, 0, len(s.targets))
	names := make([]string, 0, len(s.targets))
	for name := range s.targets {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		target := s.targets[name]
		if target == nil {
			continue
		}
		providers = append(providers, AttachmentProviderInfo{
			Name:   name,
			Source: target.target.Source,
			UI:     target.target.UI != nil,
			UIPath: target.target.UIPath,
		})
	}
	return AttachmentInfo{
		AttachID:           s.id,
		CreatedAt:          s.createdAt,
		LastSeenAt:         s.lastSeen,
		IdleTimeoutSeconds: int(DefaultSessionIdleTimeout / time.Second),
		Providers:          providers,
	}
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

func (a *AttachAuthorization) info() AttachAuthorizationInfo {
	if a == nil {
		return AttachAuthorizationInfo{}
	}
	return AttachAuthorizationInfo{
		AuthorizationID: a.id,
		Providers:       slices.Clone(a.providers),
		ExpiresAt:       a.expiresAt,
		Approved:        !a.approvedAt.IsZero(),
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
	result, err := s.invokeRaw(ctx, providerName, method, payload)
	if err != nil {
		return err
	}
	if resp == nil {
		return nil
	}
	if err := gproto.Unmarshal(result, resp); err != nil {
		return status.Errorf(codes.Internal, "unmarshal provider dev response: %v", err)
	}
	return nil
}

func (s *Session) serveUIAsset(ctx context.Context, providerName string, req UIAssetRequest) (*UIAssetResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal provider dev ui request: %v", err)
	}
	result, err := s.invokeRaw(ctx, providerName, "ServeUIAsset", payload)
	if err != nil {
		return nil, err
	}
	var resp UIAssetResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, status.Errorf(codes.Internal, "unmarshal provider dev ui response: %v", err)
	}
	return &resp, nil
}

func (s *Session) invokeRaw(ctx context.Context, providerName, method string, payload []byte) ([]byte, error) {
	if s == nil {
		return nil, status.Error(codes.Unavailable, "provider dev session is unavailable")
	}
	callID, err := randomID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create provider dev call: %v", err)
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
		return nil, status.Error(codes.Unavailable, "provider dev session is closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case result := <-call.response:
		s.touch()
		if result.err != nil {
			return nil, result.err
		}
		return result.payload, nil
	case <-s.done:
		return nil, status.Error(codes.Unavailable, "provider dev session is closed")
	case <-ctx.Done():
		s.mu.Lock()
		call.canceled = true
		delete(s.pending, callID)
		if !s.closed {
			s.lastSeen = time.Now()
		}
		s.mu.Unlock()
		return nil, ctx.Err()
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

func (p *attachProvider) PostConnect(ctx context.Context, token *core.ExternalCredential) (map[string]string, error) {
	if p == nil || p.Provider == nil {
		return nil, core.ErrPostConnectUnsupported
	}
	pcp, ok := p.Provider.(core.PostConnectCapable)
	if !ok {
		return nil, core.ErrPostConnectUnsupported
	}
	return pcp.PostConnect(ctx, token)
}

func (p *attachProvider) SupportsHTTPSubject() bool {
	return p != nil && p.Provider != nil && core.SupportsHTTPSubject(p.Provider)
}

func (p *attachProvider) ResolveHTTPSubject(ctx context.Context, req *core.HTTPSubjectResolveRequest) (*core.HTTPResolvedSubject, error) {
	if p == nil || p.Provider == nil {
		return nil, nil
	}
	subject, _, err := core.ResolveHTTPSubject(ctx, p.Provider, req)
	return subject, err
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
	BaseURL             string
	Token               string
	DispatcherSecret    string
	AuthorizationSecret string
	HTTPClient          *http.Client
}

type DispatcherOption func(*dispatcherConfig)

type dispatcherConfig struct {
	UIHandlers map[string]http.Handler
}

func WithUIHandlers(handlers map[string]http.Handler) DispatcherOption {
	return func(cfg *dispatcherConfig) {
		cfg.UIHandlers = handlers
	}
}

func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	var out CreateSessionResponse
	if err := c.doJSON(ctx, http.MethodPost, PathAttachments, req, &out); err != nil {
		return nil, err
	}
	c.DispatcherSecret = out.DispatcherSecret
	return &out, nil
}

func (c *Client) CreateAttachAuthorization(ctx context.Context, req CreateSessionRequest) (*CreateAttachAuthorizationResponse, error) {
	var out CreateAttachAuthorizationResponse
	if err := c.doJSON(ctx, http.MethodPost, PathAttachAuthorizations, req, &out); err != nil {
		return nil, err
	}
	c.AuthorizationSecret = out.ClientSecret
	return &out, nil
}

func (c Client) PollAttachAuthorization(ctx context.Context, authorizationID string) (*PollAttachAuthorizationResponse, error) {
	path := PathAttachAuthorizations + "/" + url.PathEscape(authorizationID) + "/poll"
	var out PollAttachAuthorizationResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateAuthorizedSession(ctx context.Context, authorizationID string, req CreateSessionRequest) (*CreateSessionResponse, error) {
	var out CreateSessionResponse
	path := PathAttachAuthorizations + "/" + url.PathEscape(authorizationID) + "/attachments"
	if err := c.doJSON(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	c.DispatcherSecret = out.DispatcherSecret
	c.AuthorizationSecret = ""
	return &out, nil
}

func (c Client) ListAttachments(ctx context.Context) ([]AttachmentInfo, error) {
	var out ListAttachmentsResponse
	if err := c.doJSON(ctx, http.MethodGet, PathAttachments, nil, &out); err != nil {
		return nil, err
	}
	return out.Attachments, nil
}

func (c Client) GetAttachment(ctx context.Context, attachID string) (*AttachmentInfo, error) {
	path := PathAttachments + "/" + url.PathEscape(attachID)
	var out AttachmentInfo
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c Client) Poll(ctx context.Context, sessionID string) (*PollResponse, bool, error) {
	path := PathAttachments + "/" + url.PathEscape(sessionID) + "/poll"
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
	path := PathAttachments + "/" + url.PathEscape(sessionID) + "/calls/" + url.PathEscape(callID)
	return c.doJSON(ctx, http.MethodPost, path, req, nil)
}

func (c Client) CloseSession(ctx context.Context, sessionID string) error {
	path := PathAttachments + "/" + url.PathEscape(sessionID)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

func (c Client) RunDispatcher(ctx context.Context, sessionID string, providers map[string]proto.IntegrationProviderClient, options ...DispatcherOption) error {
	cfg := dispatcherConfig{}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}
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
		req := c.dispatchCall(ctx, call, providers, cfg)
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

func (c Client) dispatchCall(ctx context.Context, call *PollResponse, providers map[string]proto.IntegrationProviderClient, cfg dispatcherConfig) CompleteCallRequest {
	if call.Method == "ServeUIAsset" {
		return dispatchProviderDevUI(ctx, call, cfg.UIHandlers)
	}
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
	return CompleteCallRequest{ResponseBase64: base64.StdEncoding.EncodeToString(resp)}
}

func dispatchProviderDevUI(ctx context.Context, call *PollResponse, handlers map[string]http.Handler) CompleteCallRequest {
	handler := handlers[strings.TrimSpace(call.Provider)]
	if handler == nil {
		return CompleteCallRequest{Error: encodeRPCError(status.Errorf(codes.NotFound, "local ui for provider %q is not running", call.Provider))}
	}
	payload, err := hex.DecodeString(call.Request)
	if err != nil {
		return CompleteCallRequest{Error: encodeRPCError(status.Errorf(codes.InvalidArgument, "decode ui request: %v", err))}
	}
	resp, err := dispatchProviderDevUIRequest(ctx, handler, payload)
	if err != nil {
		return CompleteCallRequest{Error: encodeRPCError(err)}
	}
	return CompleteCallRequest{ResponseBase64: base64.StdEncoding.EncodeToString(resp)}
}

func dispatchProviderDevUIRequest(ctx context.Context, handler http.Handler, payload []byte) ([]byte, error) {
	var req UIAssetRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode ui request: %v", err)
	}
	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		return nil, status.Errorf(codes.InvalidArgument, "provider dev ui only supports GET and HEAD requests")
	}
	path := normalizeUIAssetRequestPath(req.Path)
	target := (&url.URL{
		Scheme:   "http",
		Host:     "provider-dev.local",
		Path:     path,
		RawQuery: req.RawQuery,
	}).String()
	httpReq, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "build ui request: %v", err)
	}
	if header := cloneHTTPHeader(req.Header); header != nil {
		httpReq.Header = header
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)
	httpResp := recorder.Result()
	defer func() { _ = httpResp.Body.Close() }()
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read ui response: %v", err)
	}
	if method == http.MethodHead {
		body = nil
	}
	resp := UIAssetResponse{
		Status: httpResp.StatusCode,
		Header: cloneHTTPHeader(httpResp.Header),
		Body:   base64.StdEncoding.EncodeToString(body),
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode ui response: %v", err)
	}
	return out, nil
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
	if secret := strings.TrimSpace(c.DispatcherSecret); secret != "" {
		req.Header.Set(HeaderDispatcherSecret, secret)
	}
	if secret := strings.TrimSpace(c.AuthorizationSecret); secret != "" {
		req.Header.Set(HeaderAuthorizationSecret, secret)
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
	type requestKey struct {
		name   string
		source string
	}

	keys := make([]requestKey, 0, len(values))
	seen := map[requestKey]struct{}{}
	for i := range values {
		value := values[i]
		name := strings.TrimSpace(value.Name)
		source := strings.TrimSpace(value.Source)
		key := requestKey{name: name}
		if name == "" {
			key.source = source
		}
		if name == "" && source == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		if specName := strings.TrimSpace(value.Spec.Name); name != "" && specName != "" && specName != name {
			return nil, status.Errorf(codes.InvalidArgument, "provider spec name %q must match requested provider name %q", specName, name)
		}
		value.Name = name
		value.Source = source
		if name != "" {
			value.Spec.Name = name
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b requestKey) int {
		if c := strings.Compare(a.name, b.name); c != 0 {
			return c
		}
		return strings.Compare(a.source, b.source)
	})
	requests := make([]AttachProvider, 0, len(keys))
	for _, key := range keys {
		for i := range values {
			value := values[i]
			name := strings.TrimSpace(value.Name)
			source := strings.TrimSpace(value.Source)
			valueKey := requestKey{name: name}
			if name == "" {
				valueKey.source = source
			}
			if valueKey != key {
				continue
			}
			value.Name = name
			value.Source = source
			if name != "" {
				value.Spec.Name = name
			}
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

func cloneAttachUI(ui *AttachUI) *AttachUI {
	if ui == nil {
		return nil
	}
	out := *ui
	return &out
}

func cloneHTTPHeader(header http.Header) http.Header {
	if len(header) == 0 {
		return nil
	}
	out := make(http.Header, len(header))
	for key, values := range header {
		out[key] = slices.Clone(values)
	}
	return out
}

func normalizeUIAssetRequestPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return value
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

func cloneAnyMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
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

func newDispatcherSecret() (secret string, hash string, err error) {
	id, err := randomID()
	if err != nil {
		return "", "", err
	}
	secret = "pda_" + id
	return secret, hashDispatcherSecret(secret), nil
}

func newAttachAuthorizationSecret() (secret string, hash string, err error) {
	id, err := randomID()
	if err != nil {
		return "", "", err
	}
	secret = "pdaa_" + id
	return secret, hashAttachAuthorizationSecret(secret), nil
}

func newAttachAuthorizationCode() (code string, hash string, err error) {
	id, err := randomID()
	if err != nil {
		return "", "", err
	}
	code = "pdac_" + id
	return code, hashAttachAuthorizationSecret(code), nil
}

func newAttachAuthorizationVerificationCode() (code string, hash string, err error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	n := binary.BigEndian.Uint32(b[:]) % 1000000
	code = fmt.Sprintf("%03d-%03d", n/1000, n%1000)
	return code, hashAttachAuthorizationSecret(normalizeAttachAuthorizationVerificationCode(code)), nil
}

func hashDispatcherSecret(secret string) string {
	return hashProviderDevSecret(secret)
}

func hashAttachAuthorizationSecret(secret string) string {
	return hashProviderDevSecret(secret)
}

func hashProviderDevSecret(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func normalizeAttachAuthorizationVerificationCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range code {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() != 6 {
		return ""
	}
	return b.String()
}

func attachAuthorizationRequestHash(req CreateSessionRequest) (string, error) {
	req.AttachAuthorizationCode = ""
	payload, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func clonePrincipal(p *principal.Principal) *principal.Principal {
	if p == nil {
		return nil
	}
	clone := *p
	if p.Identity != nil {
		identity := *p.Identity
		clone.Identity = &identity
	}
	clone.Scopes = slices.Clone(p.Scopes)
	clone.TokenPermissions = nil
	clone.ActionPermissions = nil
	return principal.Canonicalized(&clone)
}
