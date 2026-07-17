package publicsurface

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
)

// Projection selects which public methods participate in a schema projection.
type Projection int

const (
	// ProjectionGRPC includes every unary PUBLIC method.
	ProjectionGRPC Projection = iota
	// ProjectionREST includes unary PUBLIC methods with HTTP bindings.
	ProjectionREST
)

// EmitPlan is the shared public-client emission plan for every language emitter.
type EmitPlan struct {
	View *View

	GRPC *model.Schema
	REST *model.Schema

	GRPCMethods []PublicMethod
	RESTMethods []PublicMethod

	GRPCReachableMessages []*model.Message
	GRPCReachableEnums    []*model.Enum
	RESTReachableMessages []*model.Message
	RESTReachableEnums    []*model.Enum

	// Legacy fields alias the gRPC projection for emitters not yet migrated.
	Filtered          *model.Schema
	MessageIndex      map[string]*model.Message
	Methods           []PublicMethod
	ReachableMessages []*model.Message
	ReachableEnums    []*model.Enum
}

// PrepareEmit validates the schema and builds the language-neutral public emit plan.
func PrepareEmit(schema *model.Schema) (*EmitPlan, error) {
	if err := Validate(schema); err != nil {
		return nil, err
	}
	view := Build(schema)
	if len(view.Services) == 0 {
		return &EmitPlan{View: view}, nil
	}

	grpcSchema, err := ProjectGRPC(schema, view)
	if err != nil {
		return nil, err
	}
	restSchema, err := ProjectREST(schema, view)
	if err != nil {
		return nil, err
	}

	grpcMessages, err := MessageIndex(schema, view)
	if err != nil {
		return nil, err
	}
	restView := RESTClientView(view)
	restMessages, err := MessageIndex(schema, restView)
	if err != nil {
		return nil, err
	}

	enums := map[string]*model.Enum{}
	for _, e := range schema.Enums {
		enums[e.FullName] = e
	}

	grpcReachableMessages, grpcReachableEnums := Reachable(grpcMessages, enums, grpcSchema.Services)
	restReachableMessages, restReachableEnums := Reachable(restMessages, enums, restSchema.Services)

	grpcMethods, err := ParseMethods(schema, view, ProjectionGRPC)
	if err != nil {
		return nil, err
	}
	restMethods, err := ParseMethods(schema, view, ProjectionREST)
	if err != nil {
		return nil, err
	}

	return &EmitPlan{
		View:                  view,
		GRPC:                  grpcSchema,
		REST:                  restSchema,
		GRPCMethods:           grpcMethods,
		RESTMethods:           restMethods,
		GRPCReachableMessages: grpcReachableMessages,
		GRPCReachableEnums:    grpcReachableEnums,
		RESTReachableMessages: restReachableMessages,
		RESTReachableEnums:    restReachableEnums,
		Filtered:              grpcSchema,
		MessageIndex:          grpcMessages,
		Methods:               grpcMethods,
		ReachableMessages:     grpcReachableMessages,
		ReachableEnums:        grpcReachableEnums,
	}, nil
}

// RESTClientView returns a view containing only REST-bound public methods.
func RESTClientView(view *View) *View {
	if view == nil {
		return nil
	}
	out := &View{
		Messages: view.Messages,
		Enums:    view.Enums,
	}
	for _, svc := range view.Services {
		var restMethods []*model.Method
		for _, m := range svc.PublicMethods {
			if m.HTTP != nil {
				restMethods = append(restMethods, m)
			}
		}
		if len(restMethods) == 0 {
			continue
		}
		out.Services = append(out.Services, &Service{
			Service:       svc.Service,
			PublicMethods: restMethods,
		})
	}
	return out
}
