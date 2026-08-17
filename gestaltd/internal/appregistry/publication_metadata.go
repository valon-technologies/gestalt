package appregistry

import (
	"fmt"
	"strings"
)

// PublicationKind identifies how a registry version was published.
type PublicationKind string

const (
	PublicationKindGitHub PublicationKind = "github"
	PublicationKindLocal  PublicationKind = "local"
)

// LocalSourceState captures optional Git working-tree provenance for local publishes.
type LocalSourceState struct {
	CommitSHA string `json:"commitSha,omitempty"`
	Dirty     bool   `json:"dirty,omitempty"`
	Untracked bool   `json:"untracked,omitempty"`
}

func cloneLocalSourceState(value *LocalSourceState) *LocalSourceState {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func normalizeLocalSourceState(value *LocalSourceState) *LocalSourceState {
	if value == nil {
		return nil
	}
	out := *value
	out.CommitSHA = strings.ToLower(strings.TrimSpace(out.CommitSHA))
	return &out
}

func validatePublicationKind(kind PublicationKind) error {
	switch kind {
	case "", PublicationKindGitHub, PublicationKindLocal:
		return nil
	default:
		return fmt.Errorf("unsupported publication kind %q", kind)
	}
}

func validateLocalSourceState(state *LocalSourceState) error {
	if state == nil {
		return nil
	}
	if strings.TrimSpace(state.CommitSHA) == "" && !state.Dirty && !state.Untracked {
		return fmt.Errorf("localSource must record commitSha and/or dirty/untracked state")
	}
	return nil
}

func publicationKindRequiresSourceRef(kind PublicationKind) bool {
	switch kind {
	case PublicationKindLocal:
		return false
	default:
		return true
	}
}
