package server

import (
	"errors"
	"testing"
)

func TestClassifyLoginFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "domain",
			err:  errors.New(`oidc auth: email domain "gmail.com" is not allowed`),
			want: loginFailureReasonDomain,
		},
		{
			name: "email verified",
			err:  errors.New("oidc auth: email user@gmail.com is not verified"),
			want: loginFailureReasonEmail,
		},
		{
			name: "generic",
			err:  errors.New("token exchange failed"),
			want: loginFailureReasonGeneric,
		},
		{
			name: "nil",
			err:  nil,
			want: loginFailureReasonGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyLoginFailure(tt.err); got != tt.want {
				t.Fatalf("classifyLoginFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoginDeniedCopy(t *testing.T) {
	t.Parallel()

	title, message := loginDeniedCopy(loginFailureReasonDomain)
	if title == "" || message == "" {
		t.Fatalf("loginDeniedCopy() returned empty copy: %q / %q", title, message)
	}
}
