package pluginpkg

import (
	"runtime"
	"testing"
)

func TestParsePlatformString_TwoComponents(t *testing.T) {
	t.Parallel()
	goos, goarch, err := ParsePlatformString("darwin/arm64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goos != "darwin" || goarch != "arm64" {
		t.Fatalf("got (%q, %q), want (darwin, arm64)", goos, goarch)
	}
}

func TestParsePlatformString_ThreeComponentsIgnoresLibC(t *testing.T) {
	t.Parallel()
	goos, goarch, err := ParsePlatformString("linux/amd64/musl")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goos != "linux" || goarch != "amd64" {
		t.Fatalf("got (%q, %q), want (linux, amd64)", goos, goarch)
	}
}

func TestParsePlatformString_RoundTrip(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"darwin/arm64", "linux/amd64", "windows/amd64"} {
		goos, goarch, err := ParsePlatformString(input)
		if err != nil {
			t.Fatalf("ParsePlatformString(%q): %v", input, err)
		}
		roundTripped := PlatformString(goos, goarch)
		if roundTripped != input {
			t.Errorf("PlatformString(ParsePlatformString(%q)) = %q", input, roundTripped)
		}
	}
}

func TestParsePlatformString_RejectsInvalid(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "darwin", "a/b/c/d", "/arm64", "darwin/"} {
		_, _, err := ParsePlatformString(input)
		if err == nil {
			t.Errorf("ParsePlatformString(%q) should fail", input)
		}
	}
}

func TestCurrentPlatformString(t *testing.T) {
	t.Parallel()
	got := CurrentPlatformString()
	if got == "" {
		t.Fatal("CurrentPlatformString() returned empty string")
	}
	goos, goarch, err := ParsePlatformString(got)
	if err != nil {
		t.Fatalf("CurrentPlatformString() returned unparseable: %v", err)
	}
	if goos != runtime.GOOS || goarch != runtime.GOARCH {
		t.Errorf("CurrentPlatformString() = %q, but runtime is %s/%s", got, runtime.GOOS, runtime.GOARCH)
	}
}
