package egress

import (
	"errors"
	"testing"
)

func TestCheckHost_ExactMatch(t *testing.T) {
	t.Parallel()
	if err := CheckHost([]string{"api.example.com"}, "api.example.com", PolicyAllow); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestCheckHost_WildcardMatch(t *testing.T) {
	t.Parallel()
	if err := CheckHost([]string{"*.example.com"}, "api.example.com", PolicyAllow); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestCheckHost_WildcardNoMatchRoot(t *testing.T) {
	t.Parallel()
	err := CheckHost([]string{"*.example.com"}, "example.com", PolicyAllow)
	if !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("expected deny for root domain with wildcard, got %v", err)
	}
}

func TestCheckHost_CaseInsensitive(t *testing.T) {
	t.Parallel()
	if err := CheckHost([]string{"API.Example.COM"}, "api.example.com", PolicyAllow); err != nil {
		t.Fatalf("expected case-insensitive match, got %v", err)
	}
}

func TestCheckHost_HostNotInList(t *testing.T) {
	t.Parallel()
	err := CheckHost([]string{"api.example.com"}, "evil.com", PolicyAllow)
	if !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("expected deny, got %v", err)
	}
}

func TestCheckHost_EmptyListDefaultAllow(t *testing.T) {
	t.Parallel()
	if err := CheckHost(nil, "anything.com", PolicyAllow); err != nil {
		t.Fatalf("expected allow with empty list and default allow, got %v", err)
	}
}

func TestCheckHost_EmptyListDefaultDeny(t *testing.T) {
	t.Parallel()
	err := CheckHost(nil, "anything.com", PolicyDeny)
	if !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("expected deny with empty list and default deny, got %v", err)
	}
}

func TestCheckHost_StripsPort(t *testing.T) {
	t.Parallel()
	if err := CheckHost([]string{"api.example.com"}, "api.example.com:8443", PolicyAllow); err != nil {
		t.Fatalf("expected allow after port stripping, got %v", err)
	}
}

func TestCheckEndpoint_DefaultPortAllowsHostnamePattern(t *testing.T) {
	t.Parallel()
	if err := CheckEndpoint([]string{"api.example.com"}, "api.example.com:443", PolicyAllow, "443"); err != nil {
		t.Fatalf("expected default port allow, got %v", err)
	}
}

func TestCheckEndpoint_DeniesNonDefaultPortWithoutExplicitAllow(t *testing.T) {
	t.Parallel()
	err := CheckEndpoint([]string{"api.example.com"}, "api.example.com:22", PolicyAllow, "443")
	if !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("expected deny for non-default port, got %v", err)
	}
}

func TestCheckEndpoint_AllowsExplicitPort(t *testing.T) {
	t.Parallel()
	if err := CheckEndpoint([]string{"api.example.com:8443"}, "api.example.com:8443", PolicyAllow, "443"); err != nil {
		t.Fatalf("expected explicit port allow, got %v", err)
	}
}

func TestCheckEndpoint_LocalhostPatternAllowsEphemeralLocalPort(t *testing.T) {
	t.Parallel()
	if err := CheckEndpoint([]string{"localhost"}, "localhost:49152", PolicyAllow, "80"); err != nil {
		t.Fatalf("expected localhost dev port allow, got %v", err)
	}
}

func TestCheckHost_EmptyDefaultActionIsAllow(t *testing.T) {
	t.Parallel()
	if err := CheckHost(nil, "anything.com", ""); err != nil {
		t.Fatalf("expected allow with empty default action, got %v", err)
	}
}
