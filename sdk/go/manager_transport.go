package gestalt

import (
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	gproto "google.golang.org/protobuf/proto"
)

func cloneRequestContext(reqCtx *proto.RequestContext) *proto.RequestContext {
	return convertProtoMessage(reqCtx, func(reqCtx *proto.RequestContext) *proto.RequestContext {
		cloned, _ := gproto.Clone(reqCtx).(*proto.RequestContext)
		return cloned
	})
}
