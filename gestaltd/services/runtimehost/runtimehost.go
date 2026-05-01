// Package runtimehost exposes the host-owned runtime process and host-service
// primitives used by executable and hosted provider runtimes.
package runtimehost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/metricutil"
	"github.com/valon-technologies/gestalt/server/services/runtimehost/runtimelogs"
	"google.golang.org/grpc"
)

type TelemetryProviders = metricutil.TelemetryProviders
type AppendRuntimeLogEntry = runtimelogs.AppendEntry

func RegisterRuntimeLogHostServer(srv *grpc.Server, runtimeProviderName string, appendLogs func(context.Context, string, string, []AppendRuntimeLogEntry) (int64, error)) {
	proto.RegisterPluginRuntimeLogHostServer(srv, NewRuntimeLogHostServer(runtimeProviderName, appendLogs))
}
