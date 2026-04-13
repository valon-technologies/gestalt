package main

import "testing"

func TestBaseDomainFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		baseURL string
		want    string
	}{
		{"https://example.com", "example.com"},
		{"http://localhost:8080", "localhost"},
		{"", "localhost"},
		{"://invalid", "localhost"},
		{"https://gestalt.example.com:443", "gestalt.example.com"},
		{"http://127.0.0.1:8080", "127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			if got := baseDomainFromURL(tt.baseURL); got != tt.want {
				t.Errorf("baseDomainFromURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestCookieDomainFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		baseURL string
		want    string
	}{
		{"https://example.com", "example.com"},
		{"http://localhost:8080", "localhost"},
		{"", ""},
		{"://invalid", ""},
		{"https://gestalt.example.com:443", "gestalt.example.com"},
		{"http://127.0.0.1:8080", ""},
	}
	for _, tt := range tests {
		t.Run(tt.baseURL, func(t *testing.T) {
			if got := cookieDomainFromURL(tt.baseURL); got != tt.want {
				t.Errorf("cookieDomainFromURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
