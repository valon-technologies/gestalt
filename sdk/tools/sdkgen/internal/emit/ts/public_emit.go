package ts

import (
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/fileset"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/model"
	"github.com/valon-technologies/gestalt/sdk/tools/sdkgen/internal/publicsurface"
)

// PublicEmitOptions configures public TypeScript emission.
type PublicEmitOptions struct {
	Imports             PublicImports
	IncludeGrpcDispatch bool
}

// EmitPublic renders the public TypeScript transport client into the caller's
// package-owned generated directory using the supplied import paths.
func EmitPublic(schema *model.Schema, imports PublicImports) (*fileset.FileSet, error) {
	return EmitPublicWithOptions(schema, PublicEmitOptions{
		Imports:             imports,
		IncludeGrpcDispatch: true,
	})
}

// EmitPublicWithOptions renders the public TypeScript transport client.
func EmitPublicWithOptions(schema *model.Schema, opts PublicEmitOptions) (*fileset.FileSet, error) {
	plan, err := publicsurface.PrepareEmit(schema)
	if err != nil {
		return nil, err
	}
	set := fileset.New()
	if len(plan.View.Services) == 0 {
		return set, nil
	}

	files := map[string]string{
		"methods.ts":              renderPublicMethods(plan.Methods),
		"types.ts":                renderPublicTypes(plan.View, opts.Imports),
		"converters.ts":           renderPublicConverters(plan.View, opts.Imports),
		"gateway_error.ts":        renderPublicGatewayError(opts.Imports),
		"rest_request_mapping.ts": renderPublicRestRequestMapping(),
		"transport_support.ts":    renderPublicTransportSupport(opts.Imports),
		"transport_kernel.ts":     renderPublicTransportKernel(opts.Imports),
		"unary_transport.ts":      renderPublicUnaryTransport(),
		"app_client.ts":           renderPublicAppClient(plan.Filtered.Services, opts.Imports),
	}
	if opts.IncludeGrpcDispatch {
		files["grpc_dispatch.ts"] = renderPublicGrpcDispatch(plan.Filtered.Services, opts.Imports)
	}
	for path, content := range files {
		if err := set.Add(path, []byte(content)); err != nil {
			return nil, err
		}
	}
	return set, nil
}
