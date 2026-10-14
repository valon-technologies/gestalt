package emit

import (
	"sort"

	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// Reachable collects every message and enum referenced transitively from the
// selected services' method inputs and outputs.
func Reachable(messagesByName map[string]*model.Message, enumsByName map[string]*model.Enum, services []*model.Service) ([]*model.Message, []*model.Enum) {
	seenMessages := map[string]bool{}
	seenEnums := map[string]bool{}
	var visit func(fullName string)
	visitRef := func(ref *model.TypeRef) {
		switch ref.Kind {
		case model.KindMessage:
			visit(ref.Message)
		case model.KindEnum:
			seenEnums[ref.Enum] = true
		}
	}
	visit = func(fullName string) {
		if seenMessages[fullName] {
			return
		}
		seenMessages[fullName] = true
		for _, f := range messagesByName[fullName].Fields {
			switch f.Kind {
			case model.KindRepeated:
				visitRef(f.Elem)
			case model.KindMap:
				visitRef(f.MapValue)
			default:
				visitRef(&model.TypeRef{Kind: f.Kind, Message: f.Message, Enum: f.Enum})
			}
		}
	}
	for _, svc := range services {
		for _, method := range svc.Methods {
			if method.Input != nil {
				visit(method.Input.FullName)
			}
			if method.Output != nil {
				visit(method.Output.FullName)
			}
		}
	}

	var messages []*model.Message
	for fullName := range seenMessages {
		messages = append(messages, messagesByName[fullName])
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].FullName < messages[j].FullName })
	var enums []*model.Enum
	for fullName := range seenEnums {
		enums = append(enums, enumsByName[fullName])
	}
	sort.Slice(enums, func(i, j int) bool { return enums[i].FullName < enums[j].FullName })
	return messages, enums
}

// HasJSONResult reports whether any method carries the json_result
// annotation, which pulls in the envelope decode runtime.
func HasJSONResult(services []*model.Service) bool {
	for _, svc := range services {
		for _, m := range svc.Methods {
			if m.JsonResult != nil {
				return true
			}
		}
	}
	return false
}
