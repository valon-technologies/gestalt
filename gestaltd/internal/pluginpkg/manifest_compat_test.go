package pluginpkg

import (
	"runtime"
	"testing"

	pluginmanifestv1 "github.com/valon-technologies/gestalt/server/sdk/pluginmanifest/v1"
)

func TestCurrentPlatformArtifact_AllowsSingleLinuxLibCSpecificArtifactWhenLibCUnknown(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:    "github.com/acme/providers/datastore",
		Version:   "0.0.1-alpha.1",
		Datastore: &pluginmanifestv1.DatastoreMetadata{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "linux",
				Arch:   runtime.GOARCH,
				LibC:   LinuxLibCGLibC,
				Path:   "gestalt-plugin-datastore",
				SHA256: "deadbeef",
			},
		},
	}

	artifact, err := CurrentPlatformArtifact(manifest, "")
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("expected error on non-linux platform")
		}
		return
	}
	if err != nil {
		t.Fatalf("CurrentPlatformArtifact: %v", err)
	}
	if artifact.LibC != LinuxLibCGLibC {
		t.Fatalf("artifact libc = %q", artifact.LibC)
	}
}

func TestCurrentPlatformArtifact_PrefersMuslWhenLinuxLibCUnknownAndMultipleSpecificArtifactsExist(t *testing.T) {
	t.Parallel()

	manifest := &pluginmanifestv1.Manifest{
		Source:    "github.com/acme/providers/datastore",
		Version:   "0.0.1-alpha.1",
		Datastore: &pluginmanifestv1.DatastoreMetadata{},
		Artifacts: []pluginmanifestv1.Artifact{
			{
				OS:     "linux",
				Arch:   runtime.GOARCH,
				LibC:   LinuxLibCGLibC,
				Path:   "gestalt-plugin-datastore-glibc",
				SHA256: "deadbeef",
			},
			{
				OS:     "linux",
				Arch:   runtime.GOARCH,
				LibC:   LinuxLibCMusl,
				Path:   "gestalt-plugin-datastore-musl",
				SHA256: "cafebabe",
			},
		},
	}

	if runtime.GOOS != "linux" {
		if _, err := CurrentPlatformArtifact(manifest, ""); err == nil {
			t.Fatal("expected error on non-linux platform")
		}
		return
	}
	artifact, err := CurrentPlatformArtifact(manifest, "")
	if err != nil {
		t.Fatalf("CurrentPlatformArtifact: %v", err)
	}
	if artifact.LibC != LinuxLibCMusl {
		t.Fatalf("artifact libc = %q, want %q", artifact.LibC, LinuxLibCMusl)
	}
}
