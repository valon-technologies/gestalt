package server

import (
	"strings"
	"testing"
)

func TestInjectPlatformBrand(t *testing.T) {
	t.Parallel()

	input := []byte(
		`<html><head><title>Gestalt</title>` +
			`<script type="application/json" id="gestalt-platform-brand">{}</script>` +
			`</head><body></body></html>`,
	)
	out := injectPlatformBrand(MountedUI{
		BrandName:    "Valon Tools",
		BrandMarkSrc: "theme/mark.svg",
	})(input)

	html := string(out)
	if !strings.Contains(html, `<title>Valon Tools</title>`) {
		t.Fatalf("title not rewritten: %s", html)
	}
	want := `{"name":"Valon Tools","markSrc":"/theme/mark.svg"}`
	if !strings.Contains(html, want) {
		t.Fatalf("brand JSON not injected: %s", html)
	}
}

func TestInjectPlatformBrandInsertsWhenMissing(t *testing.T) {
	t.Parallel()

	input := []byte(`<html><head><title>Gestalt</title></head><body></body></html>`)
	out := injectPlatformBrand(MountedUI{BrandName: "Acme"})(input)
	html := string(out)
	if !strings.Contains(html, `id="gestalt-platform-brand"`) {
		t.Fatalf("script not inserted: %s", html)
	}
	if !strings.Contains(html, `{"name":"Acme"}`) {
		t.Fatalf("brand JSON missing: %s", html)
	}
}

func TestMountedUIBrandJSONEmpty(t *testing.T) {
	t.Parallel()
	if got := string(mountedUIBrandJSON(MountedUI{})); got != "{}" {
		t.Fatalf("got %q, want {}", got)
	}
}

func TestAbsoluteBrandMarkSrc(t *testing.T) {
	t.Parallel()
	if got := absoluteBrandMarkSrc("/portal", "theme/mark.svg"); got != "/portal/theme/mark.svg" {
		t.Fatalf("got %q", got)
	}
	if got := absoluteBrandMarkSrc("/", "theme/mark.svg"); got != "/theme/mark.svg" {
		t.Fatalf("got %q", got)
	}
	if got := absoluteBrandMarkSrc("/portal", "/theme/mark.svg"); got != "/theme/mark.svg" {
		t.Fatalf("already absolute got %q", got)
	}
}
