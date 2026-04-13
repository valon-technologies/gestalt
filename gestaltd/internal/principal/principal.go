package principal

import (
	"context"

	"github.com/valon-technologies/gestalt/server/core"
)

type Source int

const (
	SourceUnknown Source = iota
	SourceSession
	SourceAPIToken
	SourceWorkloadToken
	SourceEnv
)

const IdentityPrincipal = "__identity__"

type Kind string

const (
	KindUser     Kind = "user"
	KindWorkload Kind = "workload"
)

type Principal struct {
	Identity    *core.UserIdentity
	UserID      string
	SubjectID   string
	DisplayName string
	Kind        Kind
	Source      Source
	Scopes      []string
}

func (s Source) String() string {
	switch s {
	case SourceSession:
		return "session"
	case SourceAPIToken:
		return "api_token"
	case SourceWorkloadToken:
		return "workload_token"
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

func WorkloadSubjectID(workloadID string) string {
	if workloadID == "" {
		return ""
	}
	return string(KindWorkload) + ":" + workloadID
}

func IdentitySubjectID() string {
	return "identity:" + IdentityPrincipal
}

type contextKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(contextKey{}).(*Principal)
	return p
}
