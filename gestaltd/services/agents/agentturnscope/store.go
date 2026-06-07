package agentturnscope

import (
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core"
	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
	"github.com/valon-technologies/gestalt/server/services/invocation"
)

type ConnectionBinding struct {
	Connection string
}

type Scope struct {
	ProviderName        string
	SessionID           string
	TurnID              string
	CallerKind          invocation.ProviderKind
	CallerName          string
	WorkflowRunID       string
	WorkflowStepID      string
	SubjectID           string
	CredentialSubjectID string
	Permissions         []core.AccessPermission
	ToolRefs            []coreagent.ToolRef
	ToolRefsSet         bool
	ListedTools         []coreagent.ListedTool
	Tools               []coreagent.Tool
	ToolSource          coreagent.ToolSourceMode
	Connections         []ConnectionBinding
	Revoked             bool
}

type Store struct {
	mu            sync.RWMutex
	scopes        map[string]*Scope
	sessionScopes map[string]*Scope
}

func NewStore() *Store {
	return &Store{
		scopes:        map[string]*Scope{},
		sessionScopes: map[string]*Scope{},
	}
}

func (s *Store) Put(scope Scope) error {
	scope = cloneScope(scope)
	key := scopeKey(scope.ProviderName, scope.SessionID, scope.TurnID)
	if key == "" {
		return fmt.Errorf("agent turn scope requires provider, session, and turn")
	}
	if scope.ToolRefsSet || len(scope.ToolRefs) > 0 {
		scope.ToolRefsSet = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scopes == nil {
		s.scopes = map[string]*Scope{}
	}
	s.scopes[key] = &scope
	return nil
}

func (s *Store) PutSession(scope Scope) error {
	scope = cloneScope(scope)
	key := sessionScopeKey(scope.ProviderName, scope.SessionID)
	if key == "" {
		return fmt.Errorf("agent session scope requires provider and session")
	}
	scope.TurnID = ""
	if scope.ToolRefsSet || len(scope.ToolRefs) > 0 {
		scope.ToolRefsSet = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionScopes == nil {
		s.sessionScopes = map[string]*Scope{}
	}
	s.sessionScopes[key] = &scope
	return nil
}

func (s *Store) Alias(providerName, sessionID, turnID, aliasTurnID string) error {
	key := scopeKey(providerName, sessionID, turnID)
	aliasKey := scopeKey(providerName, sessionID, aliasTurnID)
	if key == "" || aliasKey == "" || key == aliasKey {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scope := s.scopes[key]
	if scope == nil {
		return fmt.Errorf("agent turn scope not found")
	}
	s.scopes[aliasKey] = scope
	return nil
}

func (s *Store) Delete(providerName, sessionID, turnID string) {
	key := scopeKey(providerName, sessionID, turnID)
	if key == "" || s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.scopes[key]
	for existingKey, scope := range s.scopes {
		if scope == target {
			delete(s.scopes, existingKey)
		}
	}
}

func (s *Store) DeleteSession(providerName, sessionID string) {
	key := sessionScopeKey(providerName, sessionID)
	if key == "" || s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessionScopes, key)
}

func (s *Store) Revoke(providerName, sessionID, turnID string) {
	key := scopeKey(providerName, sessionID, turnID)
	if key == "" || s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope := s.scopes[key]; scope != nil {
		scope.Revoked = true
	}
}

func (s *Store) Get(providerName, sessionID, turnID string) (Scope, bool) {
	key := scopeKey(providerName, sessionID, turnID)
	if key == "" || s == nil {
		return Scope{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	scope := s.scopes[key]
	if scope == nil {
		return Scope{}, false
	}
	return cloneScope(*scope), true
}

func (s *Store) GetSession(providerName, sessionID string) (Scope, bool) {
	key := sessionScopeKey(providerName, sessionID)
	if key == "" || s == nil {
		return Scope{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	scope := s.sessionScopes[key]
	if scope == nil {
		return Scope{}, false
	}
	return cloneScope(*scope), true
}

func scopeKey(providerName, sessionID, turnID string) string {
	providerName = strings.TrimSpace(providerName)
	sessionID = strings.TrimSpace(sessionID)
	turnID = strings.TrimSpace(turnID)
	if providerName == "" || sessionID == "" || turnID == "" {
		return ""
	}
	return providerName + "\x00" + sessionID + "\x00" + turnID
}

func sessionScopeKey(providerName, sessionID string) string {
	providerName = strings.TrimSpace(providerName)
	sessionID = strings.TrimSpace(sessionID)
	if providerName == "" || sessionID == "" {
		return ""
	}
	return providerName + "\x00" + sessionID
}

func cloneScope(src Scope) Scope {
	callerKind := invocation.ProviderKind(strings.TrimSpace(string(src.CallerKind)))
	callerName := strings.TrimSpace(src.CallerName)
	return Scope{
		ProviderName:        strings.TrimSpace(src.ProviderName),
		SessionID:           strings.TrimSpace(src.SessionID),
		TurnID:              strings.TrimSpace(src.TurnID),
		CallerKind:          callerKind,
		CallerName:          callerName,
		WorkflowRunID:       strings.TrimSpace(src.WorkflowRunID),
		WorkflowStepID:      strings.TrimSpace(src.WorkflowStepID),
		SubjectID:           strings.TrimSpace(src.SubjectID),
		CredentialSubjectID: strings.TrimSpace(src.CredentialSubjectID),
		Permissions:         clonePermissions(src.Permissions),
		ToolRefs:            cloneToolRefs(src.ToolRefs),
		ToolRefsSet:         src.ToolRefsSet || len(src.ToolRefs) > 0,
		ListedTools:         cloneListedTools(src.ListedTools),
		Tools:               cloneTools(src.Tools),
		ToolSource:          src.ToolSource,
		Connections:         cloneConnectionBindings(src.Connections),
		Revoked:             src.Revoked,
	}
}

func clonePermissions(src []core.AccessPermission) []core.AccessPermission {
	if len(src) == 0 {
		return nil
	}
	out := make([]core.AccessPermission, 0, len(src))
	for i := range src {
		out = append(out, core.AccessPermission{
			App:        strings.TrimSpace(src[i].App),
			Operations: append([]string(nil), src[i].Operations...),
		})
	}
	return out
}

func cloneToolRefs(src []coreagent.ToolRef) []coreagent.ToolRef {
	if len(src) == 0 {
		return nil
	}
	out := make([]coreagent.ToolRef, 0, len(src))
	for i := range src {
		out = append(out, coreagent.ToolRef{
			System:         strings.TrimSpace(src[i].System),
			App:            strings.TrimSpace(src[i].App),
			Operation:      strings.TrimSpace(src[i].Operation),
			Connection:     strings.TrimSpace(src[i].Connection),
			Instance:       strings.TrimSpace(src[i].Instance),
			CredentialMode: core.NormalizeOptionalConnectionMode(src[i].CredentialMode),
			RunAs:          core.NormalizeRunAsSubject(src[i].RunAs),
			Title:          strings.TrimSpace(src[i].Title),
			Description:    strings.TrimSpace(src[i].Description),
		})
	}
	return out
}

func cloneListedTools(src []coreagent.ListedTool) []coreagent.ListedTool {
	if len(src) == 0 {
		return nil
	}
	out := make([]coreagent.ListedTool, 0, len(src))
	for i := range src {
		out = append(out, coreagent.ListedTool{
			ToolID:           strings.TrimSpace(src[i].ToolID),
			MCPName:          strings.TrimSpace(src[i].MCPName),
			Title:            strings.TrimSpace(src[i].Title),
			Description:      strings.TrimSpace(src[i].Description),
			Tags:             append([]string(nil), src[i].Tags...),
			SearchText:       strings.TrimSpace(src[i].SearchText),
			InputSchemaJSON:  strings.TrimSpace(src[i].InputSchemaJSON),
			OutputSchemaJSON: strings.TrimSpace(src[i].OutputSchemaJSON),
			Annotations:      src[i].Annotations,
			Ref:              cloneToolRefs([]coreagent.ToolRef{src[i].Ref})[0],
			Target:           cloneToolTarget(src[i].Target),
			Hidden:           src[i].Hidden,
		})
	}
	return out
}

func cloneTools(src []coreagent.Tool) []coreagent.Tool {
	if len(src) == 0 {
		return nil
	}
	out := make([]coreagent.Tool, 0, len(src))
	for i := range src {
		out = append(out, coreagent.Tool{
			ID:               strings.TrimSpace(src[i].ID),
			Name:             strings.TrimSpace(src[i].Name),
			Description:      strings.TrimSpace(src[i].Description),
			ParametersSchema: maps.Clone(src[i].ParametersSchema),
			Target:           cloneToolTarget(src[i].Target),
			Hidden:           src[i].Hidden,
		})
	}
	return out
}

func cloneToolTarget(src coreagent.ToolTarget) coreagent.ToolTarget {
	return coreagent.ToolTarget{
		System:         strings.TrimSpace(src.System),
		App:            strings.TrimSpace(src.App),
		Operation:      strings.TrimSpace(src.Operation),
		Connection:     strings.TrimSpace(src.Connection),
		Instance:       strings.TrimSpace(src.Instance),
		CredentialMode: core.NormalizeOptionalConnectionMode(src.CredentialMode),
		Unavailable:    cloneUnavailable(src.Unavailable),
		RunAs:          core.NormalizeRunAsSubject(src.RunAs),
	}
}

func cloneUnavailable(src *coreagent.UnavailableToolTarget) *coreagent.UnavailableToolTarget {
	if src == nil {
		return nil
	}
	return &coreagent.UnavailableToolTarget{
		Reason:  strings.TrimSpace(src.Reason),
		Message: strings.TrimSpace(src.Message),
	}
}

func cloneConnectionBindings(src []ConnectionBinding) []ConnectionBinding {
	if len(src) == 0 {
		return nil
	}
	out := make([]ConnectionBinding, 0, len(src))
	for i := range src {
		connection := strings.TrimSpace(src[i].Connection)
		if connection == "" {
			continue
		}
		out = append(out, ConnectionBinding{Connection: connection})
	}
	return out
}
