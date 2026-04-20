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
	SourceSession
	SourceAPIToken
	SourceIdentityToken
	SourceEnv
)

const SourceWorkloadToken Source = SourceIdentityToken

const IdentityPrincipal = "__identity__"
const managedIdentityLegacySubjectPrefix = "managed_identity:"

type Kind string

const (
	KindUser     Kind = "user"
	KindIdentity Kind = "identity"
)

type Principal struct {
	Identity         *core.UserIdentity
	UserID           string
	SubjectID        string
	DisplayName      string
	Kind             Kind
	Source           Source
	Scopes           []string
	TokenPermissions PermissionSet
}

type PermissionSet map[string]map[string]struct{}

func (s Source) String() string {
	switch s {
	case SourceSession:
		return "session"
	case SourceAPIToken:
		return "api_token"
	case SourceIdentityToken:
		return "identity_token"
	case SourceEnv:
		return "env"
	default:
		return ""
	}
}

func (p *Principal) AuthSource() string {
	if p == nil {
		return ""
	}
	if p.Identity == nil && p.UserID == "" && p.SubjectID == "" && p.Kind == "" && len(p.Scopes) == 0 && p.TokenPermissions == nil {
		return ""
	}
	return p.Source.String()
}

func (p *Principal) HasUserContext() bool {
	if p == nil {
		return false
	}
	if strings.TrimSpace(p.UserID) != "" {
		return true
	}
	return p.Identity != nil && strings.TrimSpace(p.Identity.Email) != ""
}
func UserSubjectID(userID string) string {
	if userID == "" {
		return ""
	}
	return string(KindUser) + ":" + userID
}

func WorkloadSubjectID(workloadID string) string {
	return IdentitySubjectID(workloadID)
}

func IdentitySubjectID(identityID string) string {
	if identityID == "" {
		return ""
	}
	return "identity:" + identityID
}

func ManagedIdentitySubjectID(identityID string) string {
	return IdentitySubjectID(identityID)
}

func ManagedIdentityIDFromSubjectID(subjectID string) string {
	switch {
	case strings.HasPrefix(subjectID, string(KindIdentity)+":"):
		return strings.TrimPrefix(subjectID, string(KindIdentity)+":")
	case strings.HasPrefix(subjectID, managedIdentityLegacySubjectPrefix):
		return strings.TrimPrefix(subjectID, managedIdentityLegacySubjectPrefix)
	default:
		return ""
	}
}

func CompilePermissions(perms []core.AccessPermission) PermissionSet {
	if len(perms) == 0 {
		return nil
	}
	set := make(PermissionSet, len(perms))
	for _, perm := range perms {
		plugin := strings.TrimSpace(perm.Plugin)
		if plugin == "" {
			continue
		}
		if len(perm.Operations) == 0 {
			set[plugin] = nil
			continue
		}
		if _, ok := set[plugin]; ok && set[plugin] == nil {
			continue
		}
		ops := set[plugin]
		if ops == nil {
			ops = map[string]struct{}{}
			set[plugin] = ops
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
		return nil
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
		perms = append(perms, core.AccessPermission{Plugin: scope})
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
	plugins := make([]string, 0, len(set))
	for plugin := range set {
		plugins = append(plugins, plugin)
	}
	sort.Strings(plugins)
	out := make([]core.AccessPermission, 0, len(plugins))
	for _, plugin := range plugins {
		ops := set[plugin]
		perm := core.AccessPermission{Plugin: plugin}
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

func PermissionPlugins(set PermissionSet) []string {
	if set == nil {
		return nil
	}
	plugins := make([]string, 0, len(set))
	for plugin := range set {
		plugins = append(plugins, plugin)
	}
	sort.Strings(plugins)
	return plugins
}

func CompileManagedIdentityGrants(grants []*core.ManagedIdentityGrant) PermissionSet {
	if len(grants) == 0 {
		return PermissionSet{}
	}
	perms := make([]core.AccessPermission, 0, len(grants))
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		perms = append(perms, core.AccessPermission{
			Plugin:     grant.Plugin,
			Operations: append([]string(nil), grant.Operations...),
		})
	}
	compiled := CompilePermissions(perms)
	if compiled == nil {
		return PermissionSet{}
	}
	return compiled
}

func AllowsProviderPermission(p *Principal, provider string) bool {
	if p == nil {
		return false
	}
	if p.TokenPermissions != nil {
		_, ok := p.TokenPermissions[provider]
		return ok
	}
	if p.Scopes == nil {
		return true
	}
	for _, scope := range p.Scopes {
		if scope == provider {
			return true
		}
	}
	return false
}

func AllowsOperationPermission(p *Principal, provider, operation string) bool {
	if p == nil {
		return false
	}
	if p.TokenPermissions != nil {
		ops, ok := p.TokenPermissions[provider]
		if !ok {
			return false
		}
		if len(ops) == 0 {
			return true
		}
		_, ok = ops[operation]
		return ok
	}
	return AllowsProviderPermission(p, provider)
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

type contextKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(contextKey{}).(*Principal)
	return p
}
