package mcpupstream

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/valon-technologies/gestalt/server/core/catalog"
	"github.com/valon-technologies/gestalt/server/services/apps/operationexposure"
)

// AllowedOperationsCheck describes what one MCP upstream advertised during a
// live tools/list call and how its allowed_operations policy matched it.
type AllowedOperationsCheck struct {
	// Upstream is the provider name of the MCP upstream.
	Upstream string `json:"upstream"`
	// Tools lists every tool name the upstream advertised, sorted.
	Tools []string `json:"tools"`
	// AllowedOperationsConfigured reports whether an allowed_operations policy
	// applies to this upstream. When false, every tool is exposed as-is.
	AllowedOperationsConfigured bool `json:"allowedOperationsConfigured"`
	// Exposed lists the tools Gestalt exposes and the name each is exposed as.
	Exposed []ExposedTool `json:"exposed"`
	// UnknownAllowedOperations lists allowed_operations keys that name no tool
	// the upstream advertised. Any entry here makes normal discovery fail.
	UnknownAllowedOperations []string `json:"unknownAllowedOperations"`
	// UnexposedTools lists advertised tools that allowed_operations hides.
	UnexposedTools []string `json:"unexposedTools"`
	// Error is set when the upstream could not be reached or listed.
	Error string `json:"error,omitempty"`
}

// ExposedTool maps an upstream tool name to its Gestalt operation name.
type ExposedTool struct {
	Tool      string `json:"tool"`
	ExposedAs string `json:"exposedAs"`
}

// OK reports whether the upstream was listed and every allowed_operations key
// matched an advertised tool.
func (c AllowedOperationsCheck) OK() bool {
	return c.Error == "" && len(c.UnknownAllowedOperations) == 0
}

// CheckReport collects AllowedOperationsCheck results from every MCP upstream
// whose discovery runs under a context returned by WithCheckReport.
type CheckReport struct {
	mu     sync.Mutex
	checks map[string]AllowedOperationsCheck
}

type checkReportKey struct{}

// WithCheckReport returns a context that puts MCP discovery into check mode:
// each upstream lists its tools live, records the result in report, and
// returns its catalog without failing on unknown allowed_operations keys.
func WithCheckReport(ctx context.Context, report *CheckReport) context.Context {
	return context.WithValue(ctx, checkReportKey{}, report)
}

func checkReportFromContext(ctx context.Context) *CheckReport {
	report, _ := ctx.Value(checkReportKey{}).(*CheckReport)
	return report
}

// Checks returns the recorded results sorted by upstream name.
func (r *CheckReport) Checks() []AllowedOperationsCheck {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AllowedOperationsCheck, 0, len(r.checks))
	for upstream := range r.checks {
		out = append(out, r.checks[upstream])
	}
	slices.SortFunc(out, func(a, b AllowedOperationsCheck) int { return strings.Compare(a.Upstream, b.Upstream) })
	return out
}

func (r *CheckReport) recordError(upstream string, err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Catalog resolution may try several connection targets for one upstream.
	// A successful listing from any of them wins over an earlier failure.
	if existing, ok := r.checks[upstream]; ok && existing.Error == "" {
		return
	}
	r.put(AllowedOperationsCheck{Upstream: upstream, Error: err.Error()})
}

func (r *CheckReport) record(upstream string, policy *operationexposure.Policy, cat *catalog.Catalog) {
	if r == nil {
		return
	}
	check := AllowedOperationsCheck{
		Upstream:                    upstream,
		Tools:                       []string{},
		AllowedOperationsConfigured: policy != nil,
		Exposed:                     []ExposedTool{},
		UnknownAllowedOperations:    policy.UnknownCatalogOperations(cat),
		UnexposedTools:              []string{},
	}
	if check.UnknownAllowedOperations == nil {
		check.UnknownAllowedOperations = []string{}
	}
	if cat != nil {
		for i := range cat.Operations {
			tool := cat.Operations[i].ID
			check.Tools = append(check.Tools, tool)
			if exposed, ok := policy.ExposedName(tool); ok {
				check.Exposed = append(check.Exposed, ExposedTool{Tool: tool, ExposedAs: exposed})
			} else {
				check.UnexposedTools = append(check.UnexposedTools, tool)
			}
		}
	}
	slices.Sort(check.Tools)
	slices.Sort(check.UnexposedTools)
	slices.SortFunc(check.Exposed, func(a, b ExposedTool) int { return strings.Compare(a.Tool, b.Tool) })

	r.mu.Lock()
	defer r.mu.Unlock()
	r.put(check)
}

func (r *CheckReport) put(check AllowedOperationsCheck) {
	if r.checks == nil {
		r.checks = make(map[string]AllowedOperationsCheck)
	}
	r.checks[check.Upstream] = check
}
