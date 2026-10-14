package emit

import (
	"path"
	"sort"
	"strings"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// FileGroup is one generated file: every service, message, and enum declared by
// one proto file.
type FileGroup struct {
	Base     string
	Services []*model.Service
	Messages []*model.Message
	Enums    []*model.Enum
}

// GroupFiles assigns services, messages, and enums to generated files by
// their declaring proto file.
func GroupFiles(services []*model.Service, messages []*model.Message, enums []*model.Enum) []*FileGroup {
	groups := map[string]*FileGroup{}
	groupFor := func(protoFile string) *FileGroup {
		base := strings.TrimSuffix(path.Base(protoFile), ".proto")
		g, ok := groups[base]
		if !ok {
			g = &FileGroup{Base: base}
			groups[base] = g
		}
		return g
	}
	for _, svc := range services {
		groupFor(svc.ProtoFile).Services = append(groupFor(svc.ProtoFile).Services, svc)
	}
	for _, m := range messages {
		groupFor(m.ProtoFile).Messages = append(groupFor(m.ProtoFile).Messages, m)
	}
	for _, e := range enums {
		groupFor(e.ProtoFile).Enums = append(groupFor(e.ProtoFile).Enums, e)
	}
	var out []*FileGroup
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out
}
