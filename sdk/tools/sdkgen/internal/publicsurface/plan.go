package publicsurface

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// EmitPlan is the shared public-client emission plan for every language emitter.
type EmitPlan struct {
	View              *View
	Filtered          *model.Schema
	MessageIndex      map[string]*model.Message
	Methods           []PublicMethod
	ReachableMessages []*model.Message
	ReachableEnums    []*model.Enum

	// SharedMessages maps each reachable message full name to true when the
	// message is field-identical to the provider schema (no fill/reject
	// omission). Emitters reference the provider type for shared messages
	// instead of regenerating it. Projected messages (absent from the map or
	// false) have stripped fields and must be defined locally.
	SharedMessages map[string]bool
}

func prepareEmitFromView(view *View, schema *model.Schema) (*EmitPlan, error) {
	if len(view.Services) == 0 {
		return &EmitPlan{View: view, SharedMessages: map[string]bool{}}, nil
	}
	filtered, err := Project(schema, view)
	if err != nil {
		return nil, err
	}
	messages, err := MessageIndex(schema, view)
	if err != nil {
		return nil, err
	}
	enums := map[string]*model.Enum{}
	for _, e := range schema.Enums {
		enums[e.FullName] = e
	}
	reachableMessages, reachableEnums := Reachable(messages, enums, filtered.Services)
	methods, err := ParseMethods(schema, view)
	if err != nil {
		return nil, err
	}
	shared := classifySharedMessages(view, reachableMessages)
	return &EmitPlan{
		View:              view,
		Filtered:          filtered,
		MessageIndex:      messages,
		Methods:           methods,
		ReachableMessages: reachableMessages,
		ReachableEnums:    reachableEnums,
		SharedMessages:    shared,
	}, nil
}

// PrepareEmit validates the schema and builds the language-neutral public emit plan.
func PrepareEmit(schema *model.Schema) (*EmitPlan, error) {
	if err := Validate(schema); err != nil {
		return nil, err
	}
	return prepareEmitFromView(Build(schema), schema)
}

// PrepareRESTEmit validates the schema and builds the browser REST emit plan.
func PrepareRESTEmit(schema *model.Schema) (*EmitPlan, error) {
	if err := Validate(schema); err != nil {
		return nil, err
	}
	return prepareEmitFromView(FilterREST(Build(schema)), schema)
}

// classifySharedMessages returns a set of reachable message full names that
// are field-identical to the provider schema — no fill/reject fields omitted.
// These messages can be referenced from the provider package instead of
// regenerated in the public client tree.
func classifySharedMessages(view *View, reachable []*model.Message) map[string]bool {
	omitByInput, err := inputFieldPolicies(view)
	if err != nil {
		return map[string]bool{}
	}
	shared := map[string]bool{}
	for _, m := range reachable {
		if len(omitByInput[m.FullName]) == 0 {
			shared[m.FullName] = true
		}
	}
	return shared
}
