package pluginsdk

import (
	"context"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginsdk/proto/v1"
	"google.golang.org/grpc"
)

type invocationIDKey struct{}

func WithInvocationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, invocationIDKey{}, id)
}

func InvocationID(ctx context.Context) string {
	id, _ := ctx.Value(invocationIDKey{}).(string)
	return id
}

type HTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

type providerHostClient struct {
	client pluginapiv1.ProviderHostClient
	conn   *grpc.ClientConn
}

func (c *providerHostClient) ProxyHTTP(ctx context.Context, invocationID, method, url string, headers map[string]string, body []byte) (*HTTPResponse, error) {
	resp, err := c.client.ProxyHTTP(ctx, &pluginapiv1.ProxyHTTPRequest{
		InvocationId: invocationID,
		Method:       method,
		Url:          url,
		Headers:      headers,
		Body:         body,
	})
	if err != nil {
		return nil, err
	}
	return &HTTPResponse{
		StatusCode: int(resp.GetStatusCode()),
		Headers:    resp.GetHeaders(),
		Body:       resp.GetBody(),
	}, nil
}

func (c *providerHostClient) Close() error {
	return c.conn.Close()
}
