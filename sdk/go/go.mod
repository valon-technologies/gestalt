module github.com/valon-technologies/gestalt/sdk/go

go 1.26

require (
	go.opentelemetry.io/otel v1.39.0
	go.opentelemetry.io/otel/metric v1.39.0
	go.opentelemetry.io/otel/trace v1.39.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d
	google.golang.org/grpc v1.80.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/valon-technologies/gestalt v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)

replace github.com/valon-technologies/gestalt => ../..
