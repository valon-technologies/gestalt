package pluginpkg

import (
	"errors"
	"fmt"
	"runtime"
)

var ErrNoSourceProviderPackage = errors.New("no source provider package found")

type SourceProviderLanguage string

const (
	SourceProviderLanguageGo     SourceProviderLanguage = "go"
	SourceProviderLanguagePython SourceProviderLanguage = "python"
)

type SourceProvider struct {
	Language     SourceProviderLanguage
	PythonTarget string
}

func DetectSourceProvider(root, goos, goarch string) (*SourceProvider, error) {
	var goToolUnavailable error
	if _, err := DetectGoProviderImportPath(root, goos, goarch); err == nil {
		return &SourceProvider{Language: SourceProviderLanguageGo}, nil
	} else if errors.Is(err, ErrGoToolUnavailable) {
		goToolUnavailable = err
	} else if !errors.Is(err, ErrNoGoProviderPackage) {
		return nil, err
	}

	if target, err := detectPythonProviderTarget(root); err == nil {
		return &SourceProvider{
			Language:     SourceProviderLanguagePython,
			PythonTarget: target,
		}, nil
	} else if !errors.Is(err, ErrNoPythonProviderPackage) {
		return nil, err
	}

	if goToolUnavailable != nil {
		return nil, goToolUnavailable
	}
	return nil, ErrNoSourceProviderPackage
}

func DetectCurrentSourceProvider(root string) (*SourceProvider, error) {
	return DetectSourceProvider(root, runtime.GOOS, runtime.GOARCH)
}

func SourceProviderRunCommand(root string) (string, []string, func(), error) {
	provider, err := DetectCurrentSourceProvider(root)
	if err != nil {
		return "", nil, nil, err
	}
	return provider.RunCommand(root)
}

func SourceProviderExecCommand(root string) (string, []string, func(), error) {
	provider, err := DetectCurrentSourceProvider(root)
	if err != nil {
		return "", nil, nil, err
	}
	return provider.ExecCommand(root)
}

func (p *SourceProvider) RunCommand(root string) (string, []string, func(), error) {
	switch p.Language {
	case SourceProviderLanguageGo:
		return GoProviderRunCommand(root)
	case SourceProviderLanguagePython:
		return pythonProviderRunCommandForTarget(root, p.PythonTarget)
	default:
		return "", nil, nil, fmt.Errorf("unsupported source provider language %q", p.Language)
	}
}

func (p *SourceProvider) ExecCommand(root string) (string, []string, func(), error) {
	switch p.Language {
	case SourceProviderLanguageGo:
		command, cleanup, err := BuildGoProviderTempBinary(root, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return "", nil, nil, err
		}
		return command, nil, cleanup, nil
	case SourceProviderLanguagePython:
		return pythonProviderRunCommandForTarget(root, p.PythonTarget)
	default:
		return "", nil, nil, fmt.Errorf("unsupported source provider language %q", p.Language)
	}
}

func (p *SourceProvider) NormalizeReleasePlatform(goos, goarch string) error {
	if p.Language != SourceProviderLanguagePython {
		return nil
	}
	return validatePythonReleasePlatform(goos, goarch)
}

func (p *SourceProvider) BuildReleaseBinary(root, binaryPath, pluginName, goos, goarch string) error {
	switch p.Language {
	case SourceProviderLanguageGo:
		return BuildGoProviderBinary(root, binaryPath, goos, goarch)
	case SourceProviderLanguagePython:
		return buildPythonProviderBinary(root, binaryPath, pluginName, p.PythonTarget, goos, goarch)
	default:
		return fmt.Errorf("unsupported source provider language %q", p.Language)
	}
}

func HasSourceProviderPackage(root string) (bool, error) {
	_, err := DetectCurrentSourceProvider(root)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoSourceProviderPackage):
		return false, nil
	default:
		return false, err
	}
}
