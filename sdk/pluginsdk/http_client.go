package pluginsdk

import (
	"context"

	pluginapiv1 "github.com/valon-technologies/gestalt/sdk/pluginapi/v1"
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

type ProxiedHTTPClient struct {
	host pluginapiv1.ProviderHostClient
}

func NewProxiedHTTPClient(host pluginapiv1.ProviderHostClient) *ProxiedHTTPClient {
	return &ProxiedHTTPClient{host: host}
}

func (c *ProxiedHTTPClient) Do(ctx context.Context, invocationID, method, url string, headers map[string]string, body []byte) (*HTTPResponse, error) {
	resp, err := c.host.ProxyHTTP(ctx, &pluginapiv1.ProxyHTTPRequest{
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
