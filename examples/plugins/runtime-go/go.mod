module github.com/valon-technologies/gestalt/examples/plugins/runtime-go

go 1.26

require (
	github.com/valon-technologies/gestalt/sdk/pluginapi v0.0.0-00010101000000-000000000000
	github.com/valon-technologies/gestalt/sdk/pluginsdk v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.10
)

require (
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)

replace (
	github.com/valon-technologies/gestalt/sdk/pluginapi => ../../../sdk/pluginapi
	github.com/valon-technologies/gestalt/sdk/pluginsdk => ../../../sdk/pluginsdk
)
