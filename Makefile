.PHONY: proto
proto:
	protoc \
		--go_out=plugins/agentic/sandbox/pb --go_opt=paths=source_relative \
		--go-grpc_out=plugins/agentic/sandbox/pb --go-grpc_opt=paths=source_relative \
		-I plugins/agentic/proto \
		plugins/agentic/proto/sandbox/v1/sandbox.proto
