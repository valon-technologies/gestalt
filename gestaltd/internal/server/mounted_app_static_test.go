package server

import (
	"strings"
	"testing"
)

func TestInjectAppContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		appName string
		mount   string
		html    string
		want    []string
		notWant []string
	}{
		{
			name:    "registered key and mount",
			appName: "ciCd",
			mount:   "/ci-cd",
			html:    `<html><head lang="en"><title>CI</title></head><body></body></html>`,
			want: []string{
				`<meta name="gestalt-app-name" content="ciCd">`,
				`<base href="/ci-cd/">`,
			},
		},
		{
			name:    "arbitrary alias unrelated to mount",
			appName: "internalDeliveryDashboard",
			mount:   "/ci-cd",
			html:    "<html><head></head></html>",
			want: []string{
				`<meta name="gestalt-app-name" content="internalDeliveryDashboard">`,
				`<base href="/ci-cd/">`,
			},
		},
		{
			name:    "root mount",
			appName: "homeApp",
			mount:   "/",
			html:    "<html><head></head></html>",
			want: []string{
				`<meta name="gestalt-app-name" content="homeApp">`,
				`<base href="/">`,
			},
		},
		{
			name:    "preserves existing base tag",
			appName: "ciCd",
			mount:   "/ci-cd",
			html:    `<html><head><base href="/custom/"></head></html>`,
			want: []string{
				`<meta name="gestalt-app-name" content="ciCd">`,
			},
			notWant: []string{`<base href="/ci-cd/">`},
		},
		{
			name:    "missing head falls back after html",
			appName: "ciCd",
			mount:   "/ci-cd",
			html:    "<html><body></body></html>",
			want: []string{
				`<meta name="gestalt-app-name" content="ciCd">`,
				`<base href="/ci-cd/">`,
			},
		},
		{
			name:    "html-sensitive app key is escaped",
			appName: `app" onclick="alert(1)`,
			mount:   "/apps",
			html:    "<html><head></head></html>",
			want: []string{
				`<meta name="gestalt-app-name" content="app&#34; onclick=&#34;alert(1)">`,
			},
		},
		{
			name:    "skips header and injects into real head",
			appName: "ciCd",
			mount:   "/ci-cd",
			html:    `<html><header>nav</header><head lang="en"></head><body></body></html>`,
			want: []string{
				`<meta name="gestalt-app-name" content="ciCd">`,
				`<head lang="en">`,
			},
			notWant: []string{"<header><meta", `<header>nav</header><meta`},
		},
		{
			name:    "html with attributes",
			appName: "ciCd",
			mount:   "/ci-cd",
			html:    `<html lang="en"><body></body></html>`,
			want: []string{
				`<meta name="gestalt-app-name" content="ciCd">`,
				`<html lang="en">`,
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := string(injectAppContext(tc.appName, tc.mount)([]byte(tc.html)))
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("output missing %q\ngot: %s", want, got)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(got, notWant) {
					t.Fatalf("output contains %q\ngot: %s", notWant, got)
				}
			}
		})
	}
}
