package principal

import (
	"context"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/server/core"
)

type Source int

const (
	SourceUnknown Source = iota
	SourceBearer
	SourceEnv
)

type Kind string

const (
	KindUser Kind = "user"
)

type Principal struct {
	Identity    *core.UserIdentity
	UserID      string
	SubjectID   string
	DisplayName string
	Kind        Kind
	Source      Source
	Scopes      []string
	ClientID    string
	Audience    []string
}

type PermissionSet map[string]map[string]struct{}

func ClonePermissionSet(src PermissionSet) PermissionSet {
	if src == nil {
		return nil
	}
	out := make(PermissionSet, len(src))
	for pluginName, operations := range src {
		if operations == nil {
			out[pluginName] = nil
			continue
		}
		copied := make(map[string]struct{}, len(operations))
		for operation := range operations {
			copied[operation] = struct{}{}
		}
		out[pluginName] = copied
	}
	return out
}

func (s Source) String() string {
	switch s {
	case SourceBearer:
		return "bearer"
	case SourceEnv:
		return "env"
	default:
		return ""
	}
}

func ParseSource(value string) Source {
	switch strings.TrimSpace(value) {
	case SourceBearer.String(), "session", "api_token":
		return SourceBearer
	case SourceEnv.String():
		return SourceEnv
	default:
		return SourceUnknown
	}
}

func (p *Principal) AuthSource() string {
	if p == nil {
		return ""
	}
	if p.Identity == nil && p.UserID == "" && p.SubjectID == "" && p.Kind == "" && len(p.Scopes) == 0 {
		return ""
	}
	return p.Source.String()
}

func UserSubjectID(userID string) string {
	if userID == "" {
		return ""
	}
	return string(KindUser) + ":" + userID
}

func UserIDFromSubjectID(subjectID string) string {
	if !strings.HasPrefix(subjectID, string(KindUser)+":") {
		return ""
	}
	return strings.TrimPrefix(subjectID, string(KindUser)+":")
}

func KindFromSubjectID(subjectID string) Kind {
	kind, _, ok := core.ParseSubjectID(subjectID)
	if !ok {
		return ""
	}
	return Kind(kind)
}

func IsSystemSubjectID(subjectID string) bool {
	return strings.HasPrefix(strings.TrimSpace(subjectID), "system:")
}

func IsSystemPrincipal(p *Principal) bool {
	return p != nil && IsSystemSubjectID(p.SubjectID)
}

func IsNonUserPrincipal(p *Principal) bool {
	p = Canonicalized(p)
	return p != nil && p.Kind != "" && p.Kind != KindUser
}

func CompilePermissions(perms []core.AccessPermission) PermissionSet {
	if len(perms) == 0 {
		return nil
	}
	set := make(PermissionSet, len(perms))
	for _, perm := range perms {
		appName := strings.TrimSpace(perm.App)
		if appName == "" {
			continue
		}
		if len(perm.Operations) == 0 {
			set[appName] = nil
			continue
		}
		if _, ok := set[appName]; ok && set[appName] == nil {
			continue
		}
		ops := set[appName]
		if ops == nil {
			ops = map[string]struct{}{}
			set[appName] = ops
		}
		for _, op := range perm.Operations {
			op = strings.TrimSpace(op)
			if op == "" {
				continue
			}
			ops[op] = struct{}{}
		}
	}
	if len(set) == 0 {
		return PermissionSet{}
	}
	return set
}

func PermissionsFromScopeString(scopes string) PermissionSet {
	if strings.TrimSpace(scopes) == "" {
		return nil
	}
	perms := make([]core.AccessPermission, 0, len(strings.Fields(scopes)))
	for _, scope := range strings.Fields(scopes) {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		perms = append(perms, core.AccessPermission{App: scope})
	}
	return CompilePermissions(perms)
}

func IntersectPermissions(a, b PermissionSet) PermissionSet {
	if a == nil || b == nil {
		return nil
	}
	out := make(PermissionSet)
	for plugin, aOps := range a {
		bOps, ok := b[plugin]
		if !ok {
			continue
		}
		switch {
		case aOps == nil && bOps == nil:
			out[plugin] = nil
		case aOps == nil:
			out[plugin] = clonePermissionOps(bOps)
		case bOps == nil:
			out[plugin] = clonePermissionOps(aOps)
		default:
			ops := make(map[string]struct{})
			for op := range aOps {
				if _, ok := bOps[op]; ok {
					ops[op] = struct{}{}
				}
			}
			if len(ops) > 0 {
				out[plugin] = ops
			}
		}
	}
	if len(out) == 0 {
		return PermissionSet{}
	}
	return out
}

func PermissionsToAccessPermissions(set PermissionSet) []core.AccessPermission {
	if set == nil {
		return nil
	}
	appNames := make([]string, 0, len(set))
	for appName := range set {
		appNames = append(appNames, appName)
	}
	sort.Strings(appNames)
	out := make([]core.AccessPermission, 0, len(appNames))
	for _, appName := range appNames {
		ops := set[appName]
		perm := core.AccessPermission{App: appName}
		if len(ops) > 0 {
			names := make([]string, 0, len(ops))
			for op := range ops {
				names = append(names, op)
			}
			sort.Strings(names)
			perm.Operations = names
		}
		out = append(out, perm)
	}
	return out
}

func PermissionApps(set PermissionSet) []string {
	if set == nil {
		return nil
	}
	appNames := make([]string, 0, len(set))
	for appName := range set {
		appNames = append(appNames, appName)
	}
	sort.Strings(appNames)
	return appNames
}

func AllowsProviderPermission(p *Principal, provider string) bool {
	if p == nil || provider == "" {
		return false
	}
	scopes := appPermissionScopes(p.Scopes)
	if len(scopes) == 0 {
		return true
	}
	prefix := provider + ":"
	for _, scope := range scopes {
		if scope == provider || strings.HasPrefix(scope, prefix) {
			return true
		}
	}
	return false
}

func AllowsOperationPermission(p *Principal, provider, operation string) bool {
	if p == nil || provider == "" || operation == "" {
		return false
	}
	scopes := appPermissionScopes(p.Scopes)
	if len(scopes) == 0 {
		return true
	}
	opScope := provider + ":" + operation
	for _, scope := range scopes {
		if scope == provider || scope == opScope {
			return true
		}
	}
	return false
}

// appPermissionScopes separates OIDC identity scopes from Gestalt app
// permissions. A browser/MCP token commonly carries openid, email, and
// profile; those scopes describe who the caller is and must not accidentally
// turn into a deny-all app permission set.
func appPermissionScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		switch strings.ToLower(strings.TrimSpace(scope)) {
		case "", "openid", "email", "profile", "offline_access":
			continue
		default:
			filtered = append(filtered, scope)
		}
	}
	return filtered
}

func clonePermissionOps(src map[string]struct{}) map[string]struct{} {
	if src == nil {
		return nil
	}
	dst := make(map[string]struct{}, len(src))
	for key := range src {
		dst[key] = struct{}{}
	}
	return dst
}

// PermissionSetFromScopes converts OAuth scope strings into a permission set.
// App-wide scopes use "<app>"; operation scopes use "<app>:<operation>".
func PermissionSetFromScopes(scopes []string) PermissionSet {
	if len(scopes) == 0 {
		return nil
	}
	set := make(PermissionSet)
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || isIdentityScope(scope) {
			continue
		}
		app, op, hasOp := strings.Cut(scope, ":")
		if hasOp && op != "" {
			ops := set[app]
			if ops == nil && set[app] != nil {
				continue
			}
			if ops == nil {
				ops = map[string]struct{}{}
				set[app] = ops
			}
			ops[op] = struct{}{}
			continue
		}
		set[scope] = nil
	}
	if len(set) == 0 {
		return PermissionSet{}
	}
	return set
}

func isIdentityScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "openid", "email", "profile", "offline_access":
		return true
	default:
		return false
	}
}

// ScopeStringsFromPermissionSet flattens a permission set into OAuth scope strings.
func ScopeStringsFromPermissionSet(set PermissionSet) []string {
	if set == nil {
		return nil
	}
	appNames := PermissionApps(set)
	out := make([]string, 0, len(appNames))
	for _, appName := range appNames {
		ops := set[appName]
		if len(ops) == 0 {
			out = append(out, appName)
			continue
		}
		opNames := make([]string, 0, len(ops))
		for op := range ops {
			opNames = append(opNames, op)
		}
		sort.Strings(opNames)
		for _, op := range opNames {
			out = append(out, appName+":"+op)
		}
	}
	return out
}

// EffectivePermissions returns the caller's permission set derived from OAuth scopes.
func (p *Principal) EffectivePermissions() PermissionSet {
	if p == nil {
		return nil
	}
	return PermissionSetFromScopes(p.Scopes)
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, Canonicalized(p))
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(contextKey{}).(*Principal)
	return Canonicalize(p)
}

func Canonicalized(p *Principal) *Principal {
	if p == nil {
		return nil
	}
	clone := *p
	return Canonicalize(&clone)
}

func Canonicalize(p *Principal) *Principal {
	if p == nil {
		return nil
	}
	if p.UserID == "" && p.SubjectID != "" {
		if userID := UserIDFromSubjectID(p.SubjectID); userID != "" {
			if strings.Contains(userID, "@") {
				if p.Identity == nil {
					p.Identity = &core.UserIdentity{Email: userID}
				}
				if p.Kind == "" {
					p.Kind = KindUser
				}
			} else {
				p.UserID = userID
				if p.Kind == "" {
					p.Kind = KindUser
				}
			}
		}
	}
	if p.Kind == "" {
		p.Kind = KindFromSubjectID(p.SubjectID)
	}
	if p.SubjectID == "" && p.UserID != "" {
		p.SubjectID = UserSubjectID(p.UserID)
		if p.Kind == "" {
			p.Kind = KindUser
		}
	}
	return p
}
