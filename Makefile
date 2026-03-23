.PHONY: proto proto-go proto-python
proto: proto-go proto-python

proto-go:
	protoc \
		--go_out=. --go_opt=module=github.com/valon-technologies/gestalt \
		--go-grpc_out=. --go-grpc_opt=module=github.com/valon-technologies/gestalt \
		-I plugins/agentic/proto \
		plugins/agentic/proto/sandbox/v1/sandbox.proto

proto-python:
	python3 -m grpc_tools.protoc \
		--python_out=sandbox/pb --grpc_python_out=sandbox/pb \
		--pyi_out=sandbox/pb \
		-I plugins/agentic/proto/sandbox/v1 \
		plugins/agentic/proto/sandbox/v1/sandbox.proto
