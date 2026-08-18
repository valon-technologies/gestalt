package appregistry

import (
	"fmt"
	"strings"

	providermanifestv1 "github.com/valon-technologies/gestalt/server/sdk/providermanifest/v1"
	"github.com/valon-technologies/gestalt/server/services/apps/source"
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

// PublishValidationOptions controls publication-kind-specific validation.
type PublishValidationOptions struct {
	PublicationKind PublicationKind
}

func ValidatePublishInput(manifest *providermanifestv1.Manifest, version, sourceRef string) error {
	return ValidatePublishInputWithOptions(manifest, version, sourceRef, PublishValidationOptions{})
}

func ValidatePublishInputWithOptions(manifest *providermanifestv1.Manifest, version, sourceRef string, opts PublishValidationOptions) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}
	if providermanifestv1.NormalizeKind(manifest.Kind) != providermanifestv1.KindApp {
		return fmt.Errorf("app registry publish only supports kind %q, got %q", providermanifestv1.KindApp, manifest.Kind)
	}
	if err := source.ValidateVersion(strings.TrimSpace(version)); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}
	if err := validatePublicationKind(opts.PublicationKind); err != nil {
		return fmt.Errorf("publication kind: %w", err)
	}
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	if publicationKindRequiresSourceRef(opts.PublicationKind) {
		if err := validateSourceRef(sourceRef); err != nil {
			return err
		}
	} else if sourceRef != "" {
		if err := validateSourceRef(sourceRef); err != nil {
			return err
		}
	}
	if strings.TrimSpace(manifest.Source) == "" {
		return fmt.Errorf("manifest source is required")
	}
	if _, _, err := parseAppSource(manifest.Source); err != nil {
		return fmt.Errorf("invalid manifest source: %w", err)
	}
	return nil
}

func validateSourceRef(sourceRef string) error {
	sourceRef = strings.TrimSpace(sourceRef)
	if len(sourceRef) != 40 {
		return fmt.Errorf("must be a 40-character commit SHA")
	}
	for _, r := range sourceRef {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return fmt.Errorf("must be a 40-character lowercase commit SHA")
		}
	}
	return nil
}

func validatePublication(publication *Publication) error {
	if publication == nil {
		return nil
	}
	if strings.TrimSpace(publication.WorkflowRunURL) == "" {
		return fmt.Errorf("workflowRunUrl is required")
	}
	hasPR := publication.TriggerPullRequest != nil
	hasCommit := publication.TriggerCommit != nil
	if hasPR == hasCommit {
		return fmt.Errorf("exactly one trigger is required")
	}
	if hasPR {
		if publication.TriggerPullRequest.Number <= 0 || strings.TrimSpace(publication.TriggerPullRequest.URL) == "" {
			return fmt.Errorf("triggerPullRequest number and URL are required")
		}
		return nil
	}
	if err := validateSourceRef(publication.TriggerCommit.SHA); err != nil {
		return fmt.Errorf("triggerCommit sha: %w", err)
	}
	if strings.TrimSpace(publication.TriggerCommit.URL) == "" {
		return fmt.Errorf("triggerCommit URL is required")
	}
	return nil
}

func validateEntrySourceRef(entry *Entry) error {
	if entry == nil {
		return fmt.Errorf("registry entry is required")
	}
	sourceRef := strings.TrimSpace(entry.SourceRef)
	if sourceRef != "" {
		if err := validateSourceRef(sourceRef); err != nil {
			return fmt.Errorf("registry entry sourceRef: %w", err)
		}
		return nil
	}
	if entry.PublicationKind == PublicationKindLocal {
		return nil
	}
	return fmt.Errorf("registry entry sourceRef is required")
}

func validateEntryRepositoryField(entry *Entry) error {
	repository := strings.TrimSpace(entry.Repository)
	if repository == "" {
		if strings.TrimSpace(entry.SourceRef) != "" {
			return fmt.Errorf("is required")
		}
		return nil
	}
	return validateEntryRepository(repository, entry.App)
}

func validateEntryPublicationMetadata(entry *Entry) error {
	if err := validatePublicationKind(entry.PublicationKind); err != nil {
		return fmt.Errorf("registry entry publicationKind: %w", err)
	}
	if err := validateLocalSourceState(entry.LocalSource); err != nil {
		return fmt.Errorf("registry entry localSource: %w", err)
	}
	return nil
}

func validateIndexVersionSourceRef(appName, version string, release IndexVersion) error {
	sourceRef := strings.TrimSpace(release.SourceRef)
	repository := strings.TrimSpace(release.Repository)
	if sourceRef != "" && repository == "" {
		return fmt.Errorf("app registry index app %q version %q sourceRef requires repository", appName, version)
	}
	if sourceRef != "" {
		if err := validateSourceRef(sourceRef); err != nil {
			return fmt.Errorf("app registry index app %q version %q sourceRef: %w", appName, version, err)
		}
	}
	if repository != "" {
		if err := validateEntryRepository(repository, appName); err != nil {
			return fmt.Errorf("app registry index app %q version %q repository: %w", appName, version, err)
		}
	}
	if release.PublicationKind == PublicationKindGitHub && sourceRef == "" {
		return fmt.Errorf("app registry index app %q version %q sourceRef is required", appName, version)
	}
	if err := validatePublicationKind(release.PublicationKind); err != nil {
		return fmt.Errorf("app registry index app %q version %q publicationKind: %w", appName, version, err)
	}
	if err := validateLocalSourceState(release.LocalSource); err != nil {
		return fmt.Errorf("app registry index app %q version %q localSource: %w", appName, version, err)
	}
	return nil
}
