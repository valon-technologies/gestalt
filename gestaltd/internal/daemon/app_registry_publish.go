package daemon

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/daemon/appregistryremote"
)

func runAppRegistryPublish(args []string, gestaltdVersion string) error {
	fs := flag.NewFlagSet("gestaltd app registry publish", flag.ContinueOnError)
	fs.Usage = func() { printAppRegistryPublishUsage(fs.Output()) }
	remote := fs.Bool("remote", false, "publish through authenticated Gestalt instead of direct GCS")
	bucket := fs.String("bucket", "", "GCS bucket name for registry uploads")
	appName := fs.String("app", "", "app name under apps/{app}/manifest.yaml")
	version := fs.String("version", "", "semantic version guard")
	ref := fs.String("ref", "", "full source commit SHA")
	workflowRunURL := fs.String("workflow-run-url", "", "GitHub Actions workflow run URL")
	triggerPRNumber := fs.Int("trigger-pr-number", 0, "pull request number that triggered publication")
	triggerPRURL := fs.String("trigger-pr-url", "", "pull request URL that triggered publication")
	triggerPRTitle := fs.String("trigger-pr-title", "", "pull request title that triggered publication")
	triggerCommitSHA := fs.String("trigger-commit-sha", "", "commit SHA that triggered publication")
	triggerCommitURL := fs.String("trigger-commit-url", "", "commit URL that triggered publication")
	dryRun := fs.Bool("dry-run", false, "print the publish plan as JSON without uploading")
	var distDirs appPublishDistDirs
	fs.Var(&distDirs, "dist-dir", "directory containing release archives (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *remote {
		if err := rejectDirectOnlyPublishFlags(*bucket, *appName, *ref, *dryRun, *workflowRunURL, *triggerPRNumber, *triggerPRURL, *triggerPRTitle, *triggerCommitSHA, *triggerCommitURL); err != nil {
			return err
		}
		return runAppRegistryRemotePublish(*version, []string(distDirs), gestaltdVersion)
	}
	return runAppPublishCommand("gestaltd app registry publish", printAppRegistryPublishUsage, args)
}

func rejectDirectOnlyPublishFlags(bucket, app, ref string, dryRun bool, workflowRunURL string, triggerPRNumber int, triggerPRURL, triggerPRTitle, triggerCommitSHA, triggerCommitURL string) error {
	switch {
	case strings.TrimSpace(bucket) != "":
		return fmt.Errorf("--bucket is only supported in direct GCS mode; omit it with --remote")
	case strings.TrimSpace(app) != "":
		return fmt.Errorf("--app is only supported in direct GCS mode; remote mode derives app identity from manifest.source")
	case strings.TrimSpace(ref) != "":
		return fmt.Errorf("--ref is only supported in direct GCS mode; remote mode uses optional local git provenance")
	case dryRun:
		return fmt.Errorf("--dry-run is only supported in direct GCS mode")
	case workflowRunURL != "" || triggerPRNumber != 0 || triggerPRURL != "" || triggerPRTitle != "" || triggerCommitSHA != "" || triggerCommitURL != "":
		return fmt.Errorf("publication trigger flags are only supported in direct GCS mode")
	default:
		return nil
	}
}

func runAppRegistryRemotePublish(version string, distDirs []string, gestaltdVersion string) error {
	baseURL, err := config.ResolveGestaltCLIURL()
	if err != nil {
		return err
	}
	token, err := config.ResolveGestaltCLIToken()
	if err != nil {
		return err
	}
	_, err = appregistryremote.Publish(context.Background(), appregistryremote.PublishInput{
		Version: version, DistDirs: distDirs, GestaltURL: baseURL, GestaltToken: token,
		BuilderVersion: gestaltdVersion, Output: os.Stdout,
		Logf: func(format string, args ...any) { progressStatus(format, args...) },
	})
	return err
}

func printAppRegistryPublishUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry publish --remote --version VERSION --dist-dir DIR [--dist-dir DIR...]")
	writeUsageLine(w, "  gestaltd app registry publish --bucket BUCKET --app APP --version VERSION --ref SHA --dist-dir DIR [--dist-dir DIR...] [publication flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Remote mode uses GESTALT_URL/GESTALT_API_KEY or credentials from `gestalt auth login`.")
	writeUsageLine(w, "Direct mode uploads to a GCS app registry bucket from the local checkout.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --remote            Publish through authenticated Gestalt")
	writeUsageLine(w, "  --app               App name (direct mode only)")
	writeUsageLine(w, "  --bucket            GCS bucket (direct mode only)")
	writeUsageLine(w, "  --dist-dir          Directory containing release archives (repeatable)")
	writeUsageLine(w, "  --dry-run           Print upload plan JSON without writing (direct mode only)")
	writeUsageLine(w, "  --ref               Full source commit SHA (direct mode only)")
	writeUsageLine(w, "  --version           Semantic version guard")
}
