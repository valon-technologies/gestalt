package pluginpkg

import (
	"errors"
	"runtime"
)

var ErrNoSourceProviderPackage = errors.New("no source provider package found")

const (
	sourceProviderKindGo         = "go"
	sourceProviderKindRust       = "rust"
	sourceProviderKindPython     = "python"
	sourceProviderKindTypeScript = "typescript"
)

func detectSourceProvider(root, goos, goarch string) (kind string, target string, err error) {
	var goToolUnavailable error
	if _, err := DetectGoProviderImportPath(root, goos, goarch); err == nil {
		return sourceProviderKindGo, "", nil
	} else if errors.Is(err, ErrGoToolUnavailable) {
		goToolUnavailable = err
	} else if !errors.Is(err, ErrNoGoProviderPackage) {
		return "", "", err
	}

	if _, err := detectRustProviderPackage(root); err == nil {
		return sourceProviderKindRust, "", nil
	} else if !errors.Is(err, ErrNoRustProviderPackage) {
		return "", "", err
	}

	target, err = DetectPythonProviderTarget(root)
	switch {
	case err == nil:
		return sourceProviderKindPython, target, nil
	case !errors.Is(err, ErrNoPythonProviderPackage):
		return "", "", err
	default:
		target, err = DetectTypeScriptProviderTarget(root)
		switch {
		case err == nil:
			return sourceProviderKindTypeScript, target, nil
		case !errors.Is(err, ErrNoTypeScriptProviderPackage):
			return "", "", err
		case goToolUnavailable != nil:
			return "", "", goToolUnavailable
		default:
			return "", "", ErrNoSourceProviderPackage
		}
	}
}

func SourceProviderExecutionCommand(root, goos, goarch string) (string, []string, func(), error) {
	kind, target, err := detectSourceProvider(root, goos, goarch)
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
	case sourceProviderKindRust:
		return rustProviderExecutionCommand(root, goos, goarch)
	case sourceProviderKindPython:
		return pythonProviderExecutionCommand(root, target)
	case sourceProviderKindTypeScript:
		return typeScriptExecutionCommand(root, target)
	default:
		return "", nil, nil, ErrNoSourceProviderPackage
	}
}

func ValidateSourceProviderRelease(root, goos, goarch, libc string) error {
	kind, _, err := detectSourceProvider(root, goos, goarch)
	if err != nil {
		return err
	}
	switch kind {
	case sourceProviderKindRust:
		return ValidateRustProviderRelease(root, goos, goarch, libc)
	case sourceProviderKindPython:
		_, err = DetectPythonInterpreter(root, goos, goarch)
		return err
	case sourceProviderKindTypeScript:
		_, err = DetectBunExecutable()
		return err
	default:
		return nil
	}
}

func BuildSourceProviderReleaseBinary(root, outputPath, pluginName, goos, goarch, libc string) (string, error) {
	kind, target, err := detectSourceProvider(root, goos, goarch)
	if err != nil {
		return "", err
	}
	switch kind {
	case sourceProviderKindGo:
		return "", BuildGoProviderBinary(root, outputPath, pluginName, goos, goarch)
	case sourceProviderKindRust:
		return BuildRustProviderBinary(root, outputPath, pluginName, goos, goarch, libc)
	case sourceProviderKindPython:
		return BuildPythonProviderBinary(root, outputPath, pluginName, target, goos, goarch)
	case sourceProviderKindTypeScript:
		return BuildTypeScriptProviderBinary(root, outputPath, pluginName, target, goos, goarch)
	default:
		return "", ErrNoSourceProviderPackage
	}
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
