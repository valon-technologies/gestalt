package providerhost

import (
	"context"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	"github.com/valon-technologies/gestalt/server/internal/runtimelogs"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
)

type runtimeLogAppendFunc func(ctx context.Context, runtimeProviderName, sessionID string, entries []runtimelogs.AppendEntry) (int64, error)

type RuntimeLogHostServer = runtimehost.RuntimeLogHostServer

func NewRuntimeLogHostServer(runtimeProviderName string, appendLogs runtimeLogAppendFunc) *RuntimeLogHostServer {
	if appendLogs == nil {
		return runtimehost.NewRuntimeLogHostServer(runtimeProviderName, nil)
	}
	return runtimehost.NewRuntimeLogHostServer(runtimeProviderName, func(ctx context.Context, runtimeProviderName, sessionID string, entries []runtimelogs.AppendEntry) (int64, error) {
		return appendLogs(ctx, runtimeProviderName, sessionID, entries)
	})
}

var _ proto.PluginRuntimeLogHostServer = (*RuntimeLogHostServer)(nil)
