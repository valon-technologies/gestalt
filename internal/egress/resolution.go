package egress

import (
	"context"
	"maps"
)

type ResolutionInput struct {
	Target     Target
	Headers    map[string]string
	Credential CredentialMaterialization
}

type Resolution struct {
	Subject    Subject
	Target     Target
	Headers    map[string]string
	Credential CredentialMaterialization
	Policy     PolicyInput
}

type Resolver struct {
	Subjects    SubjectResolver
	Policy      PolicyEnforcer
	Credentials CredentialResolver
}

func (r Resolver) Resolve(ctx context.Context, input ResolutionInput) (Resolution, error) {
	subjectResolver := r.Subjects
	if subjectResolver == nil {
		subjectResolver = ContextSubjectResolver{}
	}

	subject, err := subjectResolver.ResolveSubject(ctx, input.Target)
	if err != nil {
		return Resolution{}, err
	}

	// Policy evaluation runs before credential resolution so that denied
	// requests never trigger secret fetches or decryption.
	policy := PolicyInput{
		Subject: subject,
		Target:  input.Target,
		Headers: maps.Clone(input.Headers),
	}
	if err := EvaluatePolicy(ctx, r.Policy, policy); err != nil {
		return Resolution{}, err
	}

	credential := input.Credential
	if r.Credentials != nil && credential.Authorization == "" && len(credential.Headers) == 0 {
		resolved, err := r.Credentials.ResolveCredential(ctx, subject, input.Target)
		if err != nil {
			return Resolution{}, err
		}
		credential = resolved
	}

	headers := ApplyHeaderMutations(input.Headers, credential.Headers)

	if credential.Authorization != "" {
		delete(headers, "Authorization")
	}

	return Resolution{
		Subject:    subject,
		Target:     input.Target,
		Headers:    headers,
		Credential: CredentialMaterialization{Authorization: credential.Authorization},
		Policy:     policy,
	}, nil
}
