package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// diagnosticError carries a logical config path so command-facing callers can
// render the original YAML location without threading yaml.Node through config.
type diagnosticError struct {
	prefix  string
	path    []string
	message string
	cause   error
}

func newConfigValidationDiagnostic(path []string, message string) *diagnosticError {
	return &diagnosticError{prefix: "config validation", path: clonePath(path), message: strings.TrimSpace(message)}
}

func WrapDiagnosticError(path []string, message string, cause error) error {
	return &diagnosticError{path: clonePath(path), message: strings.TrimSpace(message), cause: cause}
}

func (e *diagnosticError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.message)
	if message == "" && e.cause != nil {
		message = e.cause.Error()
	} else if message != "" && e.cause != nil {
		message += ": " + e.cause.Error()
	}
	if path := strings.Join(e.path, "."); path != "" {
		message = path + ": " + message
	}
	if prefix := strings.TrimSpace(e.prefix); prefix != "" {
		message = prefix + ": " + message
	}
	return message
}

func (e *diagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type renderedDiagnosticError struct {
	message string
	cause   error
}

func (e renderedDiagnosticError) Error() string {
	return e.message
}

func (e renderedDiagnosticError) Unwrap() error {
	return e.cause
}

func ProviderSourcePath(kind, name string) []string {
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	switch kind {
	case "app":
		return []string{"apps", name, "source"}
	case "runtime":
		return []string{"runtime", "providers", name, "source"}
	case "ui":
		return []string{"providers", "ui", name, "source"}
	default:
		return []string{"providers", kind, name, "source"}
	}
}

func ProviderSourceFieldPath(kind, name string, fields ...string) []string {
	path := ProviderSourcePath(kind, name)
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			path = append(path, field)
		}
	}
	return path
}

func clonePath(path []string) []string {
	if len(path) == 0 {
		return nil
	}
	out := make([]string, len(path))
	copy(out, path)
	return out
}

func RenderDiagnosticError(paths []string, err error) error {
	if err == nil {
		return nil
	}
	var rendered renderedDiagnosticError
	if errors.As(err, &rendered) {
		return err
	}
	var diagnostic *diagnosticError
	if !errors.As(err, &diagnostic) {
		return err
	}
	if diagnostic == nil || len(diagnostic.path) == 0 {
		return err
	}
	snippet, ok := renderDiagnosticSnippet(paths, diagnostic.path)
	if !ok {
		return err
	}
	message := strings.TrimRight(err.Error(), "\n") + "\n\n" + snippet
	return renderedDiagnosticError{message: message, cause: err}
}

type diagnosticLocation struct {
	path   string
	line   int
	column int
	lines  []string
}

func renderDiagnosticSnippet(paths []string, diagnosticPath []string) (string, bool) {
	location, ok := findDiagnosticLocation(paths, diagnosticPath)
	if !ok {
		return "", false
	}
	if location.line <= 0 || location.line > len(location.lines) {
		return "", false
	}
	column := location.column
	if column <= 0 {
		column = 1
	}
	start := location.line - 2
	if start < 1 {
		start = 1
	}
	end := location.line + 2
	if end > len(location.lines) {
		end = len(location.lines)
	}
	width := len(fmt.Sprintf("%d", end))
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s:%d:%d\n", location.path, location.line, column)
	for lineNo := start; lineNo <= end; lineNo++ {
		marker := " "
		if lineNo == location.line {
			marker = ">"
		}
		fmt.Fprintf(&builder, "%s %*d | %s\n", marker, width, lineNo, location.lines[lineNo-1])
		if lineNo == location.line {
			fmt.Fprintf(&builder, "  %*s | %s^\n", width, "", strings.Repeat(" ", column-1))
		}
	}
	return strings.TrimRight(builder.String(), "\n"), true
}

func findDiagnosticLocation(paths []string, diagnosticPath []string) (diagnosticLocation, bool) {
	var best diagnosticLocation
	bestDepth := 0
	bestExact := false
	found := false
	for i := len(paths) - 1; i >= 0; i-- {
		path := strings.TrimSpace(paths[i])
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err != nil {
			continue
		}
		node, depth, exact := locateDiagnosticNode(&root, diagnosticPath)
		if node == nil || depth == 0 || depth < bestDepth {
			continue
		}
		line, column := nodePosition(node)
		if line <= 0 {
			continue
		}
		candidate := diagnosticLocation{
			path:   path,
			line:   line,
			column: column,
			lines:  strings.Split(string(data), "\n"),
		}
		if !found || depth > bestDepth || (exact && !bestExact) {
			best = candidate
			bestDepth = depth
			bestExact = exact
			found = true
			if exact {
				break
			}
		}
	}
	return best, found
}

func locateDiagnosticNode(root *yaml.Node, diagnosticPath []string) (*yaml.Node, int, bool) {
	node := documentValueNode(root)
	if node == nil {
		return nil, 0, false
	}
	best := node
	var lastMatchedKey *yaml.Node
	depth := 0
	for _, segment := range diagnosticPath {
		if node.Kind != yaml.MappingNode {
			return best, depth, false
		}
		key, value := mappingEntryNode(node, segment)
		if value == nil {
			if lastMatchedKey != nil {
				return lastMatchedKey, depth, false
			}
			return nil, 0, false
		}
		lastMatchedKey = key
		node = value
		best = node
		depth++
	}
	return node, depth, true
}

func mappingEntryNode(node *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	node = documentValueNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i], node.Content[i+1]
		}
	}
	return nil, nil
}

func nodePosition(node *yaml.Node) (int, int) {
	if node == nil {
		return 0, 0
	}
	if node.Line > 0 {
		return node.Line, node.Column
	}
	for _, child := range node.Content {
		if line, column := nodePosition(child); line > 0 {
			return line, column
		}
	}
	return 0, 0
}
