package tunnel

import "testing"

func TestHostFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"plain host", "http://example.com:7000", "example.com"},
		{"wss with port", "wss://frps.example:443", "frps.example"},
		{"no port", "http://localhost", "localhost"},
		{"ipv6 bracketed", "wss://[2001:db8::1]:443", "2001:db8::1"},
		{"ipv6 no port", "http://[2001:db8::1]", "2001:db8::1"},
		{"with path", "http://example.com:8080/path", "example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hostFromURL(tc.url)
			if got != tc.want {
				t.Fatalf("hostFromURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestPortFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want int
	}{
		{"explicit port", "http://example.com:7000", 7000},
		{"wss default", "wss://example.com", 443},
		{"http default", "http://example.com", 80},
		{"ipv6 with port", "wss://[2001:db8::1]:443", 443},
		{"ipv6 no port wss", "wss://[2001:db8::1]", 443},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := portFromURL(tc.url)
			if got != tc.want {
				t.Fatalf("portFromURL(%q) = %d, want %d", tc.url, got, tc.want)
			}
		})
	}
}
