module github.com/valon-technologies/gestalt/plugins/slack

go 1.26

require github.com/valon-technologies/gestalt/sdk/go v0.0.0-00010101000000-000000000000

require (
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/valon-technologies/gestalt/sdk/go => ../../sdk/go
