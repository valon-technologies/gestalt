package egress

import "context"

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
		Headers: CopyHeaders(input.Headers),
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

func CopyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[k] = v
	}
	return out
}
