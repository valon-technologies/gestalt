package host

import (
	idb "github.com/valon-technologies/gestalt/sdk/go/indexeddb"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func rpcStatusErr(st *rpcstatus.Status) error {
	if st == nil || st.GetCode() == int32(codes.OK) {
		return nil
	}
	return grpcErr(status.Error(codes.Code(st.GetCode()), st.GetMessage()))
}

func grpcErr(err error) error {
	return idb.RPCError(err)
}
