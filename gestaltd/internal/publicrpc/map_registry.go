package publicrpc

// MapRegistry is a PublicMethodRegistry backed by an explicit policy map.
type MapRegistry struct {
	byFullMethod map[string]PublicMethodPolicy
}

// NewMapRegistry returns a registry backed by an explicit policy map.
func NewMapRegistry(policies map[string]PublicMethodPolicy) *MapRegistry {
	byFullMethod := make(map[string]PublicMethodPolicy, len(policies))
	for fullMethod, policy := range policies {
		policy.FullMethod = fullMethod
		byFullMethod[fullMethod] = policy
	}
	return &MapRegistry{byFullMethod: byFullMethod}
}

func (r *MapRegistry) Lookup(fullMethod string) (PublicMethodPolicy, bool) {
	if r == nil {
		return PublicMethodPolicy{}, false
	}
	policy, ok := r.byFullMethod[fullMethod]
	return policy, ok
}

// PublicMethods returns the registered public method names.
func (r *MapRegistry) PublicMethods() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.byFullMethod))
	for fullMethod := range r.byFullMethod {
		out = append(out, fullMethod)
	}
	return out
}
