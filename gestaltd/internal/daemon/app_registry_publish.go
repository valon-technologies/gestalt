package daemon

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/valon-technologies/gestalt/server/internal/config"
	"github.com/valon-technologies/gestalt/server/internal/daemon/appregistryremote"
)

func runAppRegistryPublish(args []string, gestaltdVersion string) error {
	fs := flag.NewFlagSet("gestaltd app registry publish", flag.ContinueOnError)
	fs.Usage = func() { printAppRegistryPublishUsage(fs.Output()) }
	remote := fs.Bool("remote", false, "publish through authenticated Gestalt instead of direct GCS")
	gestaltURL := fs.String("gestalt-url", "", "Gestalt URL; overrides GESTALT_URL and gestalt CLI config")
	gestaltToken := fs.String("gestalt-token", "", "Gestalt API token; overrides GESTALT_API_KEY and gestalt CLI credentials")
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
		return runAppRegistryRemotePublish(appRegistryRemotePublishOptions{
			Version:        *version,
			DistDirs:       []string(distDirs),
			GestaltURL:     *gestaltURL,
			GestaltToken:   *gestaltToken,
			BuilderVersion: gestaltdVersion,
		})
	}
	return runAppPublishCommand(appPublishCommandOptions{
		CommandName:      "gestaltd app registry publish",
		Usage:            printAppRegistryPublishUsage,
		Bucket:           *bucket,
		AppName:          *appName,
		Version:          *version,
		Ref:              *ref,
		WorkflowRunURL:   *workflowRunURL,
		TriggerPRNumber:  *triggerPRNumber,
		TriggerPRURL:     *triggerPRURL,
		TriggerPRTitle:   *triggerPRTitle,
		TriggerCommitSHA: *triggerCommitSHA,
		TriggerCommitURL: *triggerCommitURL,
		DryRun:           *dryRun,
		DistDirs:         distDirs,
	})
}

type appRegistryRemotePublishOptions struct {
	Version        string
	DistDirs       []string
	GestaltURL     string
	GestaltToken   string
	BuilderVersion string
}

func runAppRegistryRemotePublish(opts appRegistryRemotePublishOptions) error {
	if strings.TrimSpace(opts.GestaltURL) == "" {
		url, err := config.ResolveGestaltCLIURL()
		if err != nil {
			return err
		}
		opts.GestaltURL = url
	}
	if strings.TrimSpace(opts.GestaltToken) == "" {
		token, err := config.ResolveGestaltCLIToken()
		if err != nil {
			return err
		}
		opts.GestaltToken = token
	}
	_, err := appregistryremote.Publish(context.Background(), appregistryremote.PublishInput{
		Version:        opts.Version,
		DistDirs:       opts.DistDirs,
		GestaltURL:     opts.GestaltURL,
		GestaltToken:   opts.GestaltToken,
		BuilderVersion: opts.BuilderVersion,
		Logf: func(format string, args ...any) {
			progressStatus(format, args...)
		},
	})
	return err
}

func printAppRegistryPublishUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry publish --remote --version VERSION --dist-dir DIR [--dist-dir DIR...]")
	writeUsageLine(w, "  gestaltd app registry publish --bucket BUCKET --app APP --version VERSION --ref SHA --dist-dir DIR [--dist-dir DIR...] [publication flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Publish an installable app version to the app registry.")
	writeUsageLine(w, "Remote mode authenticates to Gestalt, uploads local archives to scoped signed URLs,")
	writeUsageLine(w, "and finalizes the version without granting developers GCS access.")
	writeUsageLine(w, "Remote mode uses GESTALT_URL/GESTALT_API_KEY or credentials from `gestalt auth login`.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --remote            Publish through authenticated Gestalt")
	writeUsageLine(w, "  --gestalt-url       Override GESTALT_URL and gestalt CLI config")
	writeUsageLine(w, "  --gestalt-token     Override GESTALT_API_KEY and gestalt CLI credentials")
	writeUsageLine(w, "  --app               App name (direct GCS mode; remote mode derives identity from manifest.source)")
	writeUsageLine(w, "  --bucket            GCS bucket name or gs:// URL (direct GCS mode)")
	writeUsageLine(w, "  --dist-dir          Directory containing release archives (repeatable)")
	writeUsageLine(w, "  --dry-run           Print the upload plan as JSON without writing (direct GCS mode)")
	writeUsageLine(w, "  --ref               Full source commit SHA (direct GCS mode)")
	writeUsageLine(w, "  --workflow-run-url      Publishing GitHub Actions run")
	writeUsageLine(w, "  --trigger-pr-number     Triggering pull request number")
	writeUsageLine(w, "  --trigger-pr-url        Triggering pull request URL")
	writeUsageLine(w, "  --trigger-pr-title      Triggering pull request title")
	writeUsageLine(w, "  --trigger-commit-sha    Triggering commit SHA")
	writeUsageLine(w, "  --trigger-commit-url    Triggering commit URL")
	writeUsageLine(w, "  --version           Semantic version guard")
}
