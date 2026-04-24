package gestalt

import (
	"context"
	"fmt"
	"os"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
)

const EnvRuntimeLogHostSocket = "GESTALT_RUNTIME_LOG_SOCKET"
const EnvRuntimeLogHostSocketToken = EnvRuntimeLogHostSocket + "_TOKEN"

type RuntimeLogHostClient struct {
	client proto.PluginRuntimeLogHostClient
}

var sharedRuntimeLogHostTransport sharedManagerTransport[proto.PluginRuntimeLogHostClient]

func RuntimeLogHost() (*RuntimeLogHostClient, error) {
	target := os.Getenv(EnvRuntimeLogHostSocket)
	if target == "" {
		return nil, fmt.Errorf("runtime log host: %s is not set", EnvRuntimeLogHostSocket)
	}
	token := os.Getenv(EnvRuntimeLogHostSocketToken)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := managerTransportClient(ctx, "runtime log host", target, token, &sharedRuntimeLogHostTransport, proto.NewPluginRuntimeLogHostClient)
	if err != nil {
		return nil, err
	}
	return &RuntimeLogHostClient{client: client}, nil
}

func (c *RuntimeLogHostClient) Close() error {
	return nil
}

func (c *RuntimeLogHostClient) AppendLogs(ctx context.Context, req *proto.AppendPluginRuntimeLogsRequest) (*proto.AppendPluginRuntimeLogsResponse, error) {
	return c.client.AppendLogs(ctx, req)
}
