package egress

import "context"

type SubjectResolver interface {
	ResolveSubject(ctx context.Context, target Target) (Subject, error)
}

type subjectContextKey struct{}

func WithSubject(ctx context.Context, subject Subject) context.Context {
	return context.WithValue(ctx, subjectContextKey{}, subject)
}

func SubjectFromContext(ctx context.Context) (Subject, bool) {
	subject, ok := ctx.Value(subjectContextKey{}).(Subject)
	if !ok {
		return Subject{}, false
	}
	return subject, true
}

type ContextSubjectResolver struct{}

func (ContextSubjectResolver) ResolveSubject(ctx context.Context, _ Target) (Subject, error) {
	subject, _ := SubjectFromContext(ctx)
	return subject, nil
}
