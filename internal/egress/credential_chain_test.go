package egress_test

import (
	"context"
	"errors"
	"testing"

	"github.com/valon-technologies/gestalt/internal/egress"
)

type staticCredentialResolver struct {
	mat    egress.CredentialMaterialization
	err    error
	called bool
}

func (r *staticCredentialResolver) ResolveCredential(_ context.Context, _ egress.Subject, _ egress.Target) (egress.CredentialMaterialization, error) {
	r.called = true
	return r.mat, r.err
}

var (
	testSubject = egress.Subject{Kind: egress.SubjectUser, ID: "test-user"}
	testTarget  = egress.Target{Provider: "test-provider", Host: "api.test"}
)

func TestCredentialSourceChain_EmptyChain(t *testing.T) {
	t.Parallel()
	chain := &egress.CredentialSourceChain{}

	mat, err := chain.ResolveCredential(context.Background(), testSubject, testTarget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "" || len(mat.Headers) > 0 {
		t.Fatalf("expected empty materialization, got %+v", mat)
	}
}

func TestCredentialSourceChain_SingleSourceMatch(t *testing.T) {
	t.Parallel()
	src := &staticCredentialResolver{
		mat: egress.CredentialMaterialization{Authorization: "Bearer tok-abc"},
	}
	chain := &egress.CredentialSourceChain{Sources: []egress.CredentialResolver{src}}

	mat, err := chain.ResolveCredential(context.Background(), testSubject, testTarget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mat.Authorization != "Bearer tok-abc" {
		t.Fatalf("expected Bearer tok-abc, got %q", mat.Authorization)
	}
}

func TestCredentialSourceChain_FirstEmptySecondWins(t *testing.T) {
	t.Parallel()
	first := &staticCredentialResolver{}
	second := &staticCredentialResolver{
		mat: egress.CredentialMaterialization{Authorization: "Bearer from-second"},
	}
	chain := &egress.CredentialSourceChain{
		Sources: []egress.CredentialResolver{first, second},
	}

	mat, err := chain.ResolveCredential(context.Background(), testSubject, testTarget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !first.called {
		t.Fatal("expected first source to be called")
	}
	if !second.called {
		t.Fatal("expected second source to be called")
	}
	if mat.Authorization != "Bearer from-second" {
		t.Fatalf("expected Bearer from-second, got %q", mat.Authorization)
	}
}

func TestCredentialSourceChain_FirstMatchShortCircuits(t *testing.T) {
	t.Parallel()
	first := &staticCredentialResolver{
		mat: egress.CredentialMaterialization{Authorization: "Bearer from-first"},
	}
	second := &staticCredentialResolver{
		mat: egress.CredentialMaterialization{Authorization: "Bearer from-second"},
	}
	chain := &egress.CredentialSourceChain{
		Sources: []egress.CredentialResolver{first, second},
	}

	mat, err := chain.ResolveCredential(context.Background(), testSubject, testTarget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !first.called {
		t.Fatal("expected first source to be called")
	}
	if second.called {
		t.Fatal("expected second source to NOT be called")
	}
	if mat.Authorization != "Bearer from-first" {
		t.Fatalf("expected Bearer from-first, got %q", mat.Authorization)
	}
}

func TestCredentialSourceChain_ErrorPropagates(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("resolver failure")
	first := &staticCredentialResolver{err: sentinel}
	second := &staticCredentialResolver{
		mat: egress.CredentialMaterialization{Authorization: "Bearer unreachable"},
	}
	chain := &egress.CredentialSourceChain{
		Sources: []egress.CredentialResolver{first, second},
	}

	_, err := chain.ResolveCredential(context.Background(), testSubject, testTarget)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if second.called {
		t.Fatal("expected second source to NOT be called after error")
	}
}

func TestCredentialSourceChain_HeaderOnlyMaterialization(t *testing.T) {
	t.Parallel()
	src := &staticCredentialResolver{
		mat: egress.CredentialMaterialization{
			Headers: []egress.HeaderMutation{
				{Action: egress.HeaderActionSet, Name: "X-Api-Key", Value: "key-123"},
			},
		},
	}
	chain := &egress.CredentialSourceChain{Sources: []egress.CredentialResolver{src}}

	mat, err := chain.ResolveCredential(context.Background(), testSubject, testTarget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mat.Headers) != 1 || mat.Headers[0].Value != "key-123" {
		t.Fatalf("expected header-only materialization, got %+v", mat)
	}
}
