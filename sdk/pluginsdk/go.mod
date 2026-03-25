module github.com/valon-technologies/gestalt/sdk/pluginsdk

go 1.26

require (
	github.com/valon-technologies/gestalt/sdk/pluginapi v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.76.0
	google.golang.org/protobuf v1.36.10
)

require (
	golang.org/x/net v0.42.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
	golang.org/x/text v0.27.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250804133106-a7a43d27e69b // indirect
)

replace github.com/valon-technologies/gestalt/sdk/pluginapi => ../pluginapi
