package runtimehost

import (
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

var callerCapabilityRequiredServicePrefixes = []string{
	"/" + proto.App_ServiceDesc.ServiceName + "/",
	"/" + proto.Identity_ServiceDesc.ServiceName + "/",
	"/" + proto.Agent_ServiceDesc.ServiceName + "/",
	"/" + proto.Workflow_ServiceDesc.ServiceName + "/",
}

// CallerCapabilityRequiredMethod reports whether a host-service RPC requires an
// invocation capability with embedded caller claims.
func CallerCapabilityRequiredMethod(fullMethod string) bool {
	fullMethod = strings.TrimSpace(fullMethod)
	if fullMethod == "" {
		return false
	}
	for _, prefix := range callerCapabilityRequiredServicePrefixes {
		if strings.HasPrefix(fullMethod, prefix) {
			return true
		}
	}
	return false
}
