package bootstrap

import (
	"fmt"
	"strconv"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

const (
	hostedRuntimeMetadataImageMatch    = "runtime.imageMatch"
	hostedRuntimeMetadataExpectedImage = "runtime.expectedImage"
	hostedRuntimeMetadataCurrentImage  = "runtime.currentImage"
	hostedRuntimeMetadataActualImage   = "runtime.actualImage"
	hostedRuntimeMetadataTemplate      = "runtime.template"
)

func hostedRuntimeSessionCompatibilityReason(session *proto.RuntimeSession) string {
	if session == nil || len(session.GetMetadata()) == 0 {
		return ""
	}
	metadata := session.GetMetadata()
	actual := strings.TrimSpace(metadata[hostedRuntimeMetadataActualImage])
	current := strings.TrimSpace(metadata[hostedRuntimeMetadataCurrentImage])
	expected := strings.TrimSpace(metadata[hostedRuntimeMetadataExpectedImage])
	template := strings.TrimSpace(metadata[hostedRuntimeMetadataTemplate])

	if current != "" && actual != "" && current != actual {
		return hostedRuntimeImageMismatchReason(template, current, actual)
	}
	if match, ok := parseRuntimeImageMatch(metadata[hostedRuntimeMetadataImageMatch]); ok && !match {
		if current == "" {
			current = expected
		}
		return hostedRuntimeImageMismatchReason(template, current, actual)
	}
	return ""
}

func parseRuntimeImageMatch(raw string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return parsed, true
}

func hostedRuntimeImageMismatchReason(template, expected, actual string) string {
	if template == "" {
		template = "runtime template"
	}
	if expected == "" {
		expected = "unknown"
	}
	if actual == "" {
		actual = "unknown"
	}
	return fmt.Sprintf("%s image mismatch: expected %q, actual %q", template, expected, actual)
}
