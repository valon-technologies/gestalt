package publicsurface

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// BodyBinding classifies the google.api.http body field.
type BodyBinding int

const (
	BodyNone BodyBinding = iota
	BodyStar
)

// PublicField is one request field referenced by a public method policy or REST
// binding.
type PublicField struct {
	Name     string
	JSONName string
}

// RESTRule is the parsed google.api.http rule for a public method. Path and
// query fields are resolved at generation time.
type RESTRule struct {
	Verb         string
	PathTemplate string
	PathFields   []PublicField
	Body         BodyBinding
	QueryFields  []PublicField
}

// ServiceLocalName returns the unqualified service name from a full name.
func ServiceLocalName(fullName string) string {
	if i := strings.LastIndex(fullName, "."); i >= 0 {
		return fullName[i+1:]
	}
	return fullName
}

// FieldNames returns the protobuf field names from a list of public fields.
func FieldNames(fields []PublicField) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.Name
	}
	return out
}

const operationResultMessage = "gestalt.provider.v1.OperationResult"

// ResponseIsOperationResult reports whether a method returns OperationResult.
func ResponseIsOperationResult(pm PublicMethod) bool {
	return pm.Output != nil && pm.Output.FullName == operationResultMessage
}

// PublicMethod is the parsed public method used by every language emitter.
type PublicMethod struct {
	Service    string
	Method     string
	FullMethod string

	Input         *model.Message
	Output        *model.Message
	InputIsEmpty  bool
	OutputIsEmpty bool
	REST          *RESTRule // nil means gRPC-only

	ServerFilled []PublicField
	Rejected     []PublicField
	JSONResult   *model.JsonResult
}

var pathSegmentPattern = regexp.MustCompile(`\{([^}/]+)\}`)

// ParseMethods builds the public method model from a validated view.
func ParseMethods(schema *model.Schema, view *View, projection Projection) ([]PublicMethod, error) {
	if view == nil {
		return nil, nil
	}
	var out []PublicMethod
	for _, svc := range view.Services {
		for _, m := range svc.PublicMethods {
			if projection == ProjectionREST && m.HTTP == nil {
				continue
			}
			pm, err := parseMethod(schema, svc.Service, m)
			if err != nil {
				return nil, err
			}
			out = append(out, pm)
		}
	}
	return out, nil
}

func parseMethod(schema *model.Schema, svc *model.Service, m *model.Method) (PublicMethod, error) {
	pm := PublicMethod{
		Service:       svc.FullName,
		Method:        m.Name,
		FullMethod:    m.FullMethod,
		Input:         m.Input,
		Output:        m.Output,
		InputIsEmpty:  m.InputIsEmpty,
		OutputIsEmpty: m.OutputIsEmpty,
		JSONResult:    m.JsonResult,
	}
	if m.PublicPolicy != nil {
		for _, name := range m.PublicPolicy.Fill {
			pm.ServerFilled = append(pm.ServerFilled, publicField(m.Input, name))
		}
		for _, name := range m.PublicPolicy.Reject {
			pm.Rejected = append(pm.Rejected, publicField(m.Input, name))
		}
	}
	if m.HTTP == nil {
		return pm, nil
	}
	omitted := OmittedFields(m)
	rest, err := parseRESTRule(m, m.HTTP, omitted)
	if err != nil {
		return PublicMethod{}, fmt.Errorf("%s: %w", m.FullMethod, err)
	}
	pm.REST = rest
	return pm, nil
}

func publicField(input *model.Message, name string) PublicField {
	f := PublicField{Name: name}
	if input != nil {
		for _, field := range input.Fields {
			if field.Name == name {
				f.JSONName = field.JSONName
				break
			}
		}
	}
	if f.JSONName == "" {
		f.JSONName = name
	}
	return f
}

func parseRESTRule(m *model.Method, rule *model.HTTPRule, omitted map[string]bool) (*RESTRule, error) {
	pathFields, err := parsePathTemplate(m.FullMethod, m.Input, rule.Path, omitted)
	if err != nil {
		return nil, err
	}
	var body BodyBinding
	switch rule.Body {
	case "":
		body = BodyNone
	case "*":
		body = BodyStar
	default:
		return nil, fmt.Errorf(
			"google.api.http body %q is not supported (only \"\" and \"*\" are allowed)",
			rule.Body,
		)
	}
	queryFields, err := computeQueryFields(m.Input, pathFields, body, omitted)
	if err != nil {
		return nil, err
	}
	return &RESTRule{
		Verb:         rule.Verb,
		PathTemplate: rule.Path,
		PathFields:   pathFields,
		Body:         body,
		QueryFields:  queryFields,
	}, nil
}

func computeQueryFields(
	input *model.Message,
	pathFields []PublicField,
	body BodyBinding,
	omitted map[string]bool,
) ([]PublicField, error) {
	if input == nil || body == BodyStar {
		return nil, nil
	}
	pathNames := map[string]bool{}
	for _, pf := range pathFields {
		pathNames[pf.Name] = true
	}
	var out []PublicField
	for _, f := range input.Fields {
		if omitted[f.Name] || pathNames[f.Name] {
			continue
		}
		out = append(out, PublicField{Name: f.Name, JSONName: f.JSONName})
	}
	return out, nil
}

func parsePathTemplate(fullMethod string, input *model.Message, path string, omitted map[string]bool) ([]PublicField, error) {
	if strings.Contains(path, "{") && strings.Contains(path, "=") {
		return nil, fmt.Errorf("google.api.http path %q uses complex bindings", path)
	}
	if err := validatePathBraces(path); err != nil {
		return nil, fmt.Errorf("method %s: %w", fullMethod, err)
	}
	matches := pathSegmentPattern.FindAllStringSubmatch(path, -1)
	if strings.Contains(path, "{") && len(matches) == 0 {
		return nil, fmt.Errorf("method %s: malformed google.api.http path %q", fullMethod, path)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []PublicField
	for _, match := range matches {
		name := match[1]
		if name == "" {
			return nil, fmt.Errorf("method %s: empty path binding in %q", fullMethod, path)
		}
		if strings.Contains(name, ".") {
			return nil, fmt.Errorf("google.api.http path %q uses nested field %q", path, name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		field := fieldByName(input, name)
		if field == nil {
			return nil, fmt.Errorf("google.api.http path %q references unknown field %q", path, name)
		}
		if omitted[name] {
			return nil, fmt.Errorf(
				"google.api.http path %q references omitted public field %q",
				path,
				name,
			)
		}
		if err := validatePathField(fullMethod, field); err != nil {
			return nil, err
		}
		out = append(out, PublicField{Name: field.Name, JSONName: field.JSONName})
	}
	return out, nil
}

func validatePathField(fullMethod string, f *model.Field) error {
	switch f.Kind {
	case model.KindScalar:
		if f.Scalar != model.ScalarString {
			return fmt.Errorf("method %s path field %q must be a string", fullMethod, f.Name)
		}
		return nil
	case model.KindEnum:
		return nil
	default:
		return fmt.Errorf("method %s path field %q must be a scalar or enum", fullMethod, f.Name)
	}
}

func fieldByName(m *model.Message, name string) *model.Field {
	if m == nil {
		return nil
	}
	for _, f := range m.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func validatePathBraces(path string) error {
	depth := 0
	bindingStart := -1
	for i, r := range path {
		switch r {
		case '{':
			if depth > 0 {
				return fmt.Errorf("nested bindings in path %q", path)
			}
			depth = 1
			bindingStart = i + 1
		case '}':
			if depth == 0 {
				return fmt.Errorf("unmatched } in path %q", path)
			}
			if bindingStart >= 0 && i == bindingStart {
				return fmt.Errorf("empty path binding in %q", path)
			}
			depth = 0
			bindingStart = -1
		}
	}
	if depth != 0 {
		return fmt.Errorf("unclosed path binding in %q", path)
	}
	return nil
}
