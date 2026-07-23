package appregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

const pendingPublishWorkflowStatuses = "queued,in_progress,waiting"

var (
	squashMergePRTitleRe = regexp.MustCompile(`^(.+?)\s+\(#([0-9]+)\)\s*$`)
	mergeCommitPRTitleRe = regexp.MustCompile(`(?i)^Merge pull request #([0-9]+)`)
)

// PendingPublish describes an in-flight app registry publish workflow run.
type PendingPublish struct {
	WorkflowRunURL  string                  `json:"workflowRunUrl"`
	WorkflowStatus  string                  `json:"workflowStatus"`
	SourceRef       string                  `json:"sourceRef,omitempty"`
	ExpectedVersion string                  `json:"expectedVersion,omitempty"`
	StartedAt       time.Time               `json:"startedAt"`
	TriggerPullRequest *PublicationPullRequest `json:"triggerPullRequest,omitempty"`
}

// PublishMonitorConfig identifies the GitHub workflow that publishes snapshots.
type PublishMonitorConfig struct {
	Repository string `yaml:"repository,omitempty"`
	Workflow   string `yaml:"workflow,omitempty"`
	Branch     string `yaml:"branch,omitempty"`
}

// PublishMonitor lists in-flight publish workflow runs from GitHub Actions.
type PublishMonitor struct {
	HTTPClient *http.Client
	Token      string
	APIBaseURL string
}

func (c PublishMonitorConfig) normalized() (PublishMonitorConfig, error) {
	repository := strings.TrimSpace(c.Repository)
	workflow := strings.TrimSpace(c.Workflow)
	branch := strings.TrimSpace(c.Branch)
	if repository == "" || workflow == "" {
		return PublishMonitorConfig{}, fmt.Errorf("repository and workflow are required")
	}
	repository = strings.TrimPrefix(repository, "https://github.com/")
	repository = strings.TrimPrefix(repository, "github.com/")
	repository = strings.Trim(repository, "/")
	if !strings.Contains(repository, "/") {
		return PublishMonitorConfig{}, fmt.Errorf("repository must be owner/repo")
	}
	if branch == "" {
		branch = "main"
	}
	return PublishMonitorConfig{
		Repository: repository,
		Workflow:   workflow,
		Branch:     branch,
	}, nil
}

func (m *PublishMonitor) client() *http.Client {
	if m != nil && m.HTTPClient != nil {
		return m.HTTPClient
	}
	return http.DefaultClient
}

func (m *PublishMonitor) token() string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Token)
}

// ListPending returns publish workflow runs that have not produced an indexed snapshot yet.
func (m *PublishMonitor) ListPending(
	ctx context.Context,
	cfg PublishMonitorConfig,
	publishedVersions map[string]struct{},
) ([]PendingPublish, error) {
	normalized, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	if m.token() == "" {
		return nil, nil
	}

	runs, err := m.fetchWorkflowRuns(ctx, normalized)
	if err != nil {
		return nil, err
	}

	pending := make([]PendingPublish, 0, len(runs))
	for _, run := range runs {
		if !strings.HasSuffix(strings.TrimSpace(run.Path), normalized.Workflow) {
			continue
		}
		sourceRef := strings.ToLower(strings.TrimSpace(run.HeadSHA))
		if sourceRef == "" {
			continue
		}
		expectedVersion := SnapshotVersion(sourceRef)
		if _, exists := publishedVersions[expectedVersion]; exists {
			continue
		}
		item := PendingPublish{
			WorkflowRunURL:  strings.TrimSpace(run.HTMLURL),
			WorkflowStatus:  strings.TrimSpace(run.Status),
			SourceRef:       sourceRef,
			ExpectedVersion: expectedVersion,
			StartedAt:       run.CreatedAt.UTC(),
		}
		if pr := publicationPullRequestFromCommitTitle(cfg.Repository, run.DisplayTitle); pr != nil {
			item.TriggerPullRequest = pr
		}
		pending = append(pending, item)
	}

	sort.Slice(pending, func(i, j int) bool {
		return pending[i].StartedAt.After(pending[j].StartedAt)
	})
	return pending, nil
}

// SnapshotVersion returns the canonical snapshot version string for a source ref.
func SnapshotVersion(sourceRef string) string {
	sourceRef = strings.ToLower(strings.TrimSpace(sourceRef))
	return "0.0.0-snapshot.g" + sourceRef
}

func publicationPullRequestFromCommitTitle(repository, title string) *PublicationPullRequest {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	if matches := squashMergePRTitleRe.FindStringSubmatch(title); len(matches) == 3 {
		number, err := parsePositiveInt(matches[2])
		if err != nil {
			return nil
		}
		return &PublicationPullRequest{
			Number: number,
			Title:  strings.TrimSpace(matches[1]),
			URL:    githubPullRequestURL(repository, number),
		}
	}
	if matches := mergeCommitPRTitleRe.FindStringSubmatch(title); len(matches) == 2 {
		number, err := parsePositiveInt(matches[1])
		if err != nil {
			return nil
		}
		return &PublicationPullRequest{
			Number: number,
			URL:    githubPullRequestURL(repository, number),
		}
	}
	return nil
}

func githubPullRequestURL(repository string, number int) string {
	repository = strings.TrimPrefix(strings.TrimSpace(repository), "https://github.com/")
	repository = strings.TrimPrefix(repository, "github.com/")
	repository = strings.Trim(repository, "/")
	return fmt.Sprintf("https://github.com/%s/pull/%d", repository, number)
}

func parsePositiveInt(value string) (int, error) {
	var number int
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid number %q", value)
		}
		number = number*10 + int(r-'0')
	}
	if number <= 0 {
		return 0, fmt.Errorf("invalid number %q", value)
	}
	return number, nil
}

type githubWorkflowRunsResponse struct {
	WorkflowRuns []githubWorkflowRun `json:"workflow_runs"`
}

type githubWorkflowRun struct {
	HeadSHA      string    `json:"head_sha"`
	HTMLURL      string    `json:"html_url"`
	Status       string    `json:"status"`
	DisplayTitle string    `json:"display_title"`
	Path         string    `json:"path"`
	CreatedAt    time.Time `json:"created_at"`
}

func (m *PublishMonitor) fetchWorkflowRuns(ctx context.Context, cfg PublishMonitorConfig) ([]githubWorkflowRun, error) {
	baseURL := "https://api.github.com"
	if m != nil && strings.TrimSpace(m.APIBaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(m.APIBaseURL), "/")
	}
	url := fmt.Sprintf(
		"%s/repos/%s/actions/runs?branch=%s&event=push&status=%s&per_page=10",
		baseURL,
		cfg.Repository,
		cfg.Branch,
		pendingPublishWorkflowStatuses,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build github workflow runs request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+m.token())
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := m.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch github workflow runs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read github workflow runs: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch github workflow runs: unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload githubWorkflowRunsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode github workflow runs: %w", err)
	}
	return payload.WorkflowRuns, nil
}
