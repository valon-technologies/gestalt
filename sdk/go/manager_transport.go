package gestalt

import (
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

func cloneRequestContext(reqCtx *proto.RequestContext) *proto.RequestContext {
	if reqCtx == nil {
		return nil
	}
	cloned, _ := gproto.Clone(reqCtx).(*proto.RequestContext)
	return cloned
}
