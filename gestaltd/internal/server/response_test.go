package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/valon-technologies/gestalt/server/core"
)

func TestWriteOperationResultForwardsHeaders(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	headers.Set("Content-Type", "text/plain")
	headers.Set("Location", "/next")
	headers.Add("Set-Cookie", "a=1")
	headers.Add("Set-Cookie", "b=2")

	rec := httptest.NewRecorder()
	writeOperationResult(rec, &core.OperationResult{
		Status:  http.StatusFound,
		Headers: headers,
		Body:    "redirect",
	})

	resp := rec.Result()
	if got := resp.StatusCode; got != http.StatusFound {
		t.Fatalf("status = %d, want %d", got, http.StatusFound)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	if got := resp.Header.Get("Location"); got != "/next" {
		t.Fatalf("Location = %q, want /next", got)
	}
	if got := resp.Header.Values("Set-Cookie"); !reflect.DeepEqual(got, []string{"a=1", "b=2"}) {
		t.Fatalf("Set-Cookie = %#v, want %#v", got, []string{"a=1", "b=2"})
	}
}

func TestWriteOperationResultDefaultsContentType(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeOperationResult(rec, &core.OperationResult{
		Status: http.StatusOK,
		Body:   `{}`,
	})

	if got := rec.Result().Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}
