package publicts

import (
	"path"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func localName(fullName string) string {
	if i := strings.LastIndex(fullName, "."); i >= 0 {
		return fullName[i+1:]
	}
	return fullName
}

func generatedFileBase(protoFile string) string {
	return strings.TrimSuffix(path.Base(protoFile), ".proto")
}

func serviceClientName(svc *model.Service) string {
	return "Public" + localName(svc.FullName) + "Client"
}

func publicRequestTypeName(svc *model.Service, m *model.Method) string {
	return "Public" + localName(svc.FullName) + m.Name + "Request"
}

func methodKey(svc *model.Service, m *model.Method) string {
	return lowerFirst(localName(svc.FullName)) + "." + lowerFirst(m.Name)
}

func sortedStrings(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
