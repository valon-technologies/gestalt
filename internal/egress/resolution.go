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
	Subjects SubjectResolver
	Policy   PolicyEnforcer
	// CredentialSources is the preferred credential resolution entrypoint.
	// Credentials is kept for compatibility with existing callsites/tests.
	CredentialSources CredentialResolver
	Credentials       CredentialResolver
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

	credential := input.Credential
	credentialResolver := r.CredentialSources
	if credentialResolver == nil {
		credentialResolver = r.Credentials
	}
	if credentialResolver != nil && !hasCredentialMaterialization(credential) {
		resolved, err := credentialResolver.ResolveCredential(ctx, subject, input.Target)
		if err != nil {
			return Resolution{}, err
		}
		credential = resolved
	}

	headers := ApplyHeaderMutations(input.Headers, credential.Headers)

	if credential.Authorization != "" {
		delete(headers, "Authorization")
	}

	policyHeaders := CopyHeaders(headers)
	if credential.Authorization != "" {
		if policyHeaders == nil {
			policyHeaders = map[string]string{}
		}
		policyHeaders["Authorization"] = credential.Authorization
	}

	policy := PolicyInput{
		Subject: subject,
		Target:  input.Target,
		Headers: policyHeaders,
	}
	if err := EvaluatePolicy(ctx, r.Policy, policy); err != nil {
		return Resolution{}, err
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

func hasCredentialMaterialization(credential CredentialMaterialization) bool {
	return credential.Authorization != "" || len(credential.Headers) > 0
}
