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

	headers := ApplyHeaderMutations(input.Headers, input.Credential.Headers)

	if input.Credential.Authorization != "" {
		delete(headers, "Authorization")
	}

	policyHeaders := CopyHeaders(headers)
	if input.Credential.Authorization != "" {
		if policyHeaders == nil {
			policyHeaders = map[string]string{}
		}
		policyHeaders["Authorization"] = input.Credential.Authorization
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
		Credential: CredentialMaterialization{Authorization: input.Credential.Authorization},
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
