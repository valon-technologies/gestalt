package gestalt_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	gproto "google.golang.org/protobuf/proto"

	"github.com/valon-technologies/gestalt/sdk/go/publicclient/generated"
	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type clientCase struct {
	ID            string         `json:"id"`
	Method        string         `json:"method"`
	PublicRequest map[string]any `json:"publicRequest"`
	WireRequest   map[string]any `json:"wireRequest"`
	Response      struct {
		OperationResult *struct {
			Status     int32          `json:"status"`
			Headers    map[string]any `json:"headers"`
			BodyBase64 string         `json:"bodyBase64"`
		} `json:"operationResult"`
		GestaltError *struct {
			Code    int32  `json:"code"`
			Message string `json:"message"`
		} `json:"gestaltError"`
	} `json:"response"`
	Expect struct {
		Result       any `json:"result"`
		GestaltError *struct {
			Code    int32  `json:"code"`
			Message string `json:"message"`
		} `json:"gestaltError"`
		Calls int `json:"calls"`
	} `json:"expect"`
}

type recordingTransport struct {
	calls       int
	err         error
	body        []byte
	lastRequest gproto.Message
}

func (t *recordingTransport) Unary(_ context.Context, method generated.Method, request, response gproto.Message) error {
	t.calls++
	t.lastRequest = request
	if method.Name != "Invoke" {
		return errors.New("unexpected method: " + method.Name)
	}
	if t.err != nil {
		return t.err
	}
	out, ok := response.(*proto.OperationResult)
	if !ok {
		return errors.New("unexpected response type")
	}
	out.Status = 200
	out.Body = t.body
	return nil
}

func loadClientCases(t *testing.T) []clientCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "public_conformance", "client_cases.json"))
	if err != nil {
		t.Fatalf("read client_cases.json: %v", err)
	}
	var cases []clientCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("unmarshal client_cases.json: %v", err)
	}
	return cases
}

func wireRequestJSON(t *testing.T, msg gproto.Message) map[string]any {
	t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal wire request: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal wire request: %v", err)
	}
	return out
}

func TestPublicAppClientSharedCases(t *testing.T) {
	for _, tc := range loadClientCases(t) {
		t.Run(tc.ID, func(t *testing.T) {
			rec := &recordingTransport{}
			switch tc.ID {
			case "invoke_success":
				body, err := base64.StdEncoding.DecodeString(tc.Response.OperationResult.BodyBase64)
				if err != nil {
					t.Fatalf("decode body: %v", err)
				}
				rec.body = body
			case "platform_error":
				rec.err = &generated.GestaltError{
					Code:    generated.GestaltErrorCode(tc.Response.GestaltError.Code),
					Message: tc.Response.GestaltError.Message,
				}
			default:
				t.Fatalf("unknown case %q", tc.ID)
			}

			client := generated.NewAppClient(rec)
			request := &generated.AppInvokeRequest{
				App:       tc.PublicRequest["app"].(string),
				Operation: tc.PublicRequest["operation"].(string),
			}
			if params, ok := tc.PublicRequest["params"].(map[string]any); ok {
				request.Params = params
			}

			switch tc.ID {
			case "invoke_success":
				got, err := client.Invoke(context.Background(), request)
				if err != nil {
					t.Fatalf("Invoke: %v", err)
				}
				if rec.lastRequest == nil {
					t.Fatal("transport did not receive a request")
				}
				gotWire := wireRequestJSON(t, rec.lastRequest)
				if !reflect.DeepEqual(gotWire, tc.WireRequest) {
					t.Fatalf("transport request = %#v, want %#v", gotWire, tc.WireRequest)
				}
				if !jsonEqual(got, tc.Expect.Result) {
					t.Fatalf("result = %#v, want %#v", got, tc.Expect.Result)
				}
			case "platform_error":
				_, err := client.Invoke(context.Background(), request)
				if rec.lastRequest == nil {
					t.Fatal("transport did not receive a request")
				}
				gotWire := wireRequestJSON(t, rec.lastRequest)
				if !reflect.DeepEqual(gotWire, tc.WireRequest) {
					t.Fatalf("transport request = %#v, want %#v", gotWire, tc.WireRequest)
				}
				var gerr *generated.GestaltError
				if !errors.As(err, &gerr) {
					t.Fatalf("Invoke error = %v, want *generated.GestaltError", err)
				}
				if int32(gerr.Code) != tc.Expect.GestaltError.Code || gerr.Message != tc.Expect.GestaltError.Message {
					t.Fatalf("GestaltError = %+v, want code=%d message=%q", gerr, tc.Expect.GestaltError.Code, tc.Expect.GestaltError.Message)
				}
			}

			if rec.calls != tc.Expect.Calls {
				t.Fatalf("transport calls = %d, want %d", rec.calls, tc.Expect.Calls)
			}
		})
	}
}

func jsonEqual(left, right any) bool {
	leftBytes, _ := json.Marshal(left)
	rightBytes, _ := json.Marshal(right)
	return string(leftBytes) == string(rightBytes)
}
