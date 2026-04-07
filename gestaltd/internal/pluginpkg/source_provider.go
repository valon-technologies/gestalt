package pluginpkg

import (
	"errors"
	"fmt"
	"runtime"
)

var ErrNoSourceProviderPackage = errors.New("no source provider package found")

const (
	sourceProviderKindGo     = "go"
	sourceProviderKindPython = "python"
)

func detectSourceProvider(root, goos, goarch string) (kind string, pythonTarget string, err error) {
	var goToolUnavailable error
	if _, err := DetectGoProviderImportPath(root, goos, goarch); err == nil {
		return sourceProviderKindGo, "", nil
	} else if errors.Is(err, ErrGoToolUnavailable) {
		goToolUnavailable = err
	} else if !errors.Is(err, ErrNoGoProviderPackage) {
		return "", "", err
	}

	target, err := DetectPythonProviderTarget(root)
	switch {
	case err == nil:
		return sourceProviderKindPython, target, nil
	case !errors.Is(err, ErrNoPythonProviderPackage):
		return "", "", err
	case goToolUnavailable != nil:
		return "", "", goToolUnavailable
	default:
		return "", "", ErrNoSourceProviderPackage
	}
}

func SourceProviderExecutionCommand(root, goos, goarch string) (string, []string, func(), error) {
	kind, pythonTarget, err := detectSourceProvider(root, goos, goarch)
	if err != nil {
		return "", nil, nil, err
	}
	switch kind {
	case sourceProviderKindGo:
		command, cleanup, err := BuildGoProviderTempBinary(root, goos, goarch)
		if err != nil {
			return "", nil, nil, err
		}
		return command, nil, cleanup, nil
	case sourceProviderKindPython:
		return pythonProviderExecutionCommand(root, pythonTarget)
	default:
		return "", nil, nil, ErrNoSourceProviderPackage
	}
}

func SourceProviderCurrentPlatformOnly(root, goos, goarch string) (bool, error) {
	kind, _, err := detectSourceProvider(root, goos, goarch)
	if err != nil {
		return false, err
	}
	return kind == sourceProviderKindPython, nil
}

func ValidateSourceProviderRelease(root, goos, goarch string) error {
	kind, _, err := detectSourceProvider(root, goos, goarch)
	if err != nil {
		return err
	}
	return validateSourceProviderReleaseKind(kind, goos, goarch)
}

func BuildSourceProviderReleaseBinary(root, outputPath, pluginName, goos, goarch string) error {
	kind, pythonTarget, err := detectSourceProvider(root, goos, goarch)
	if err != nil {
		return err
	}
	switch kind {
	case sourceProviderKindGo:
		return BuildGoProviderBinary(root, outputPath, pluginName, goos, goarch)
	case sourceProviderKindPython:
		if err := validateSourceProviderReleaseKind(kind, goos, goarch); err != nil {
			return err
		}
		return BuildPythonProviderBinary(root, outputPath, pluginName, pythonTarget)
	default:
		return ErrNoSourceProviderPackage
	}
}

func validateSourceProviderReleaseKind(kind, goos, goarch string) error {
	if kind == sourceProviderKindPython && (goos != runtime.GOOS || goarch != runtime.GOARCH) {
		return fmt.Errorf("python source plugins can only be released for the current platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return nil
}

func HasSourceProviderPackage(root string) (bool, error) {
	_, _, err := detectSourceProvider(root, runtime.GOOS, runtime.GOARCH)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoSourceProviderPackage):
		return false, nil
	default:
		return false, err
	}
}
