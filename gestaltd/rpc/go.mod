module github.com/valon-technologies/gestalt/server/rpc

go 1.26

require (
	github.com/valon-technologies/gestalt/sdk/go v0.0.0-00010101000000-000000000000
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)

replace github.com/valon-technologies/gestalt/sdk/go => ../../sdk/go
