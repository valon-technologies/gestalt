package client

import (
	"reflect"
	"testing"
)

func TestListWorkflowProviderRunsResponseCodecRoundTrip(t *testing.T) {
	t.Parallel()
	total := int64(7)
	in := &ListWorkflowProviderRunsResponse{
		NextPageToken: "next",
		TotalCount:    &total,
		StatusCounts: &WorkflowRunStatusCounts{
			Pending:   1,
			Running:   2,
			Succeeded: 3,
			Failed:    0,
			Canceled:  1,
		},
		Runs: []*WorkflowRun{{Id: "run-1"}},
	}
	out := FromWireListWorkflowProviderRunsResponse(ToWireListWorkflowProviderRunsResponse(in))
	if out.NextPageToken != "next" {
		t.Fatalf("NextPageToken = %q", out.NextPageToken)
	}
	if out.TotalCount == nil || *out.TotalCount != 7 {
		t.Fatalf("TotalCount = %v", out.TotalCount)
	}
	if out.StatusCounts == nil || !reflect.DeepEqual(*out.StatusCounts, *in.StatusCounts) {
		t.Fatalf("StatusCounts = %#v, want %#v", out.StatusCounts, in.StatusCounts)
	}
	if len(out.Runs) != 1 || out.Runs[0].Id != "run-1" {
		t.Fatalf("Runs = %#v", out.Runs)
	}

	absent := FromWireListWorkflowProviderRunsResponse(ToWireListWorkflowProviderRunsResponse(&ListWorkflowProviderRunsResponse{}))
	if absent.TotalCount != nil || absent.StatusCounts != nil {
		t.Fatalf("absent aggregates should stay nil: total=%v counts=%v", absent.TotalCount, absent.StatusCounts)
	}
}

func TestListWorkflowProviderRunsRequestCodecKnownApps(t *testing.T) {
	t.Parallel()
	in := &ListWorkflowProviderRunsRequest{
		PageSize:  10,
		TargetApp: "foo",
		KnownApps: []string{"foo", "foo_bar"},
		Provider:  "local",
	}
	out := FromWireListWorkflowProviderRunsRequest(ToWireListWorkflowProviderRunsRequest(in))
	if out.TargetApp != "foo" || out.Provider != "local" || out.PageSize != 10 {
		t.Fatalf("scalar fields = %#v", out)
	}
	if !reflect.DeepEqual(out.KnownApps, []string{"foo", "foo_bar"}) {
		t.Fatalf("KnownApps = %#v", out.KnownApps)
	}
}
