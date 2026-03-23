.PHONY: proto

proto:
	protoc \
		--go_out=internal/sandbox/pb --go_opt=paths=source_relative \
		--go-grpc_out=internal/sandbox/pb --go-grpc_opt=paths=source_relative \
		-I proto \
		proto/sandbox/v1/sandbox.proto
	mv internal/sandbox/pb/sandbox/v1/*.go internal/sandbox/pb/
	rm -rf internal/sandbox/pb/sandbox
