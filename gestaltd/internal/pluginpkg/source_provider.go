package pluginpkg

import "errors"

var ErrNoSourceProviderPackage = errors.New("no source provider package found")

func SourceProviderRunCommand(root string) (string, []string, func(), error) {
	var goToolUnavailable error
	if command, args, cleanup, err := GoProviderRunCommand(root); err == nil {
		return command, args, cleanup, nil
	} else if errors.Is(err, ErrGoToolUnavailable) {
		goToolUnavailable = err
	} else if !errors.Is(err, ErrNoGoProviderPackage) {
		return "", nil, nil, err
	}

	if command, args, cleanup, err := PythonProviderRunCommand(root); err == nil {
		return command, args, cleanup, nil
	} else if !errors.Is(err, ErrNoPythonProviderPackage) {
		return "", nil, nil, err
	}

	if goToolUnavailable != nil {
		return "", nil, nil, goToolUnavailable
	}
	return "", nil, nil, ErrNoSourceProviderPackage
}

func HasSourceProviderPackage(root string) (bool, error) {
	var goToolUnavailable error
	hasGoProvider, err := HasGoProviderPackage(root)
	if errors.Is(err, ErrGoToolUnavailable) {
		goToolUnavailable = err
	} else if err != nil {
		return false, err
	}
	if hasGoProvider {
		return true, nil
	}

	_, err = DetectPythonProviderTarget(root)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNoPythonProviderPackage):
		if goToolUnavailable != nil {
			return false, goToolUnavailable
		}
		return false, nil
	default:
		return false, err
	}
}
