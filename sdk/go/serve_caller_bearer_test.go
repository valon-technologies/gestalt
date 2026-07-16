package gestalt_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	gestalt "github.com/valon-technologies/gestalt/sdk/go"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type callerBearerCaptureProvider struct{}

type callerBearerCaptureOutput struct {
	ProviderToken        string `json:"provider_token"`
	CallerBearerToken    string `json:"caller_bearer_token"`
	IdentityContextToken string `json:"identity_context_token"`
}

var callerBearerCaptureRouter = gestalt.MustRouter(
	gestalt.Register(
		gestalt.Operation[stubInput, callerBearerCaptureOutput]{
			ID:     "capture_tokens",
			Method: http.MethodPost,
		},
		(*callerBearerCaptureProvider).captureTokens,
	),
)

func (p *callerBearerCaptureProvider) Configure(context.Context, string, map[string]any) error {
	return nil
}

func (p *callerBearerCaptureProvider) captureTokens(
	ctx context.Context,
	_ stubInput,
	req gestalt.Request,
) (gestalt.Response[callerBearerCaptureOutput], error) {
	return gestalt.OK(callerBearerCaptureOutput{
		ProviderToken:        req.Token,
		CallerBearerToken:    gestalt.IdentityCallContextFromContext(ctx).CallerBearerToken,
		IdentityContextToken: gestalt.IdentityCallContextFromContext(ctx).CallerBearerToken,
	}), nil
}

func TestProviderServerExecutePreservesDistinctCallerBearer(t *testing.T) {
	t.Parallel()

	client := newAppProviderClient(t, &callerBearerCaptureProvider{}, callerBearerCaptureRouter)
	params, err := structpb.NewStruct(map[string]any{})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	resp, err := client.Execute(context.Background(), &proto.ExecuteRequest{
		Operation:         "capture_tokens",
		Params:            params,
		Token:             "provider-oauth-token",
		CallerBearerToken: "caller-bearer-token",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.GetStatus() != http.StatusOK {
		t.Fatalf("Status = %d, want %d", resp.GetStatus(), http.StatusOK)
	}
	var out callerBearerCaptureOutput
	if err := json.Unmarshal(resp.GetBody(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ProviderToken != "provider-oauth-token" {
		t.Fatalf("provider token = %q, want %q", out.ProviderToken, "provider-oauth-token")
	}
	if out.CallerBearerToken != "caller-bearer-token" {
		t.Fatalf("caller bearer token = %q, want %q", out.CallerBearerToken, "caller-bearer-token")
	}
}
