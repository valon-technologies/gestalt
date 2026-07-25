package daemon

import (
	"io"
)

func runAppRegistryPublish(args []string) error {
	return runAppPublishCommand("gestaltd app registry publish", printAppRegistryPublishUsage, args)
}

func printAppRegistryPublishUsage(w io.Writer) {
	writeUsageLine(w, "Usage:")
	writeUsageLine(w, "  gestaltd app registry publish --bucket BUCKET --app APP --version VERSION --ref SHA --dist-dir DIR [--dist-dir DIR...] [publication flags]")
	writeUsageLine(w, "")
	writeUsageLine(w, "Publish an installable app version to a GCS app registry bucket.")
	writeUsageLine(w, "Resolves the source manifest at apps/{app}/manifest.yaml under the git root.")
	writeUsageLine(w, "Builds immutable version metadata and artifacts under apps/{app}/ in the bucket.")
	writeUsageLine(w, "")
	writeUsageLine(w, "Flags:")
	writeUsageLine(w, "  --app        App name (manifest at apps/{app}/manifest.yaml)")
	writeUsageLine(w, "  --bucket     GCS bucket name or gs:// URL")
	writeUsageLine(w, "  --dist-dir   Directory containing release archives (repeatable)")
	writeUsageLine(w, "  --dry-run    Print the upload plan as JSON without writing")
	writeUsageLine(w, "  --ref        Full source commit SHA")
	writeUsageLine(w, "  --workflow-run-url      Publishing GitHub Actions run")
	writeUsageLine(w, "  --trigger-pr-number     Triggering pull request number")
	writeUsageLine(w, "  --trigger-pr-url        Triggering pull request URL")
	writeUsageLine(w, "  --trigger-pr-title      Triggering pull request title")
	writeUsageLine(w, "  --trigger-commit-sha    Triggering commit SHA")
	writeUsageLine(w, "  --trigger-commit-url    Triggering commit URL")
	writeUsageLine(w, "  --version    Semantic version guard")
}
