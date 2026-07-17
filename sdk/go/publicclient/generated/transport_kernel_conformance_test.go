package generated

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	protov1 "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
)

type transportKernelCase struct {
	ID                  string         `json:"id"`
	Request             map[string]any `json:"request"`
	OverrideQueryFields []struct {
		Name     string `json:"name"`
		JSONName string `json:"jsonName"`
	} `json:"overrideQueryFields"`
	OverrideHttpBody *string `json:"overrideHttpBody"`
	ExpectPrepare    *struct {
		Verb  string          `json:"verb"`
		Path  string          `json:"path"`
		Query [][]string      `json:"query"`
		Body  *json.RawMessage `json:"body"`
	} `json:"expectPrepare"`
	ExpectDecode *struct {
		Status            int            `json:"status"`
		BodyBase64        string         `json:"bodyBase64"`
		HeaderKeys        []string       `json:"headerKeys"`
		HeaderValueCounts map[string]int `json:"headerValueCounts"`
	} `json:"expectDecode"`
	ExpectGatewayError *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"expectGatewayError"`
	RawResponse *struct {
		Status     int        `json:"status"`
		BodyText   string     `json:"bodyText"`
		Headers    [][]string `json:"headers"`
		BodyBase64 string     `json:"bodyBase64"`
	} `json:"rawResponse"`
	ExpectGestaltError *struct {
		Code int `json:"code"`
	} `json:"expectGestaltError"`
}

func loadTransportKernelCases(t *testing.T) []transportKernelCase {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "public_conformance", "transport_kernel_cases.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cases []transportKernelCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return cases
}

func TestFixtureCasesAreCovered(t *testing.T) {
	t.Parallel()
	for _, tc := range loadTransportKernelCases(t) {
		if tc.ExpectPrepare == nil && tc.ExpectDecode == nil && tc.ExpectGatewayError == nil && tc.ExpectGestaltError == nil {
			t.Fatalf("case %q has no expectations", tc.ID)
		}
	}
}

func TestPrepareCasesFromFixture(t *testing.T) {
	t.Parallel()
	for _, tc := range loadTransportKernelCases(t) {
		if tc.ExpectPrepare == nil || tc.Request == nil {
			continue
		}
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			method := methodForPrepareCase(MethodAppInvoke, tc)
			path, err := buildRESTPath(method, tc.Request)
			if err != nil {
				t.Fatalf("buildRESTPath: %v", err)
			}
			query := buildRESTQuery(method, tc.Request)
			body, err := buildRESTBody(method, tc.Request)
			if err != nil {
				t.Fatalf("buildRESTBody: %v", err)
			}
			if method.HTTPVerb != tc.ExpectPrepare.Verb {
				t.Fatalf("method verb = %q want fixture %q", method.HTTPVerb, tc.ExpectPrepare.Verb)
			}
			if path != tc.ExpectPrepare.Path {
				t.Fatalf("path = %q want %q", path, tc.ExpectPrepare.Path)
			}
			gotQuery := make([][]string, len(query))
			for i, pair := range query {
				gotQuery[i] = []string{pair.Name, pair.Value}
			}
			if !reflect.DeepEqual(gotQuery, tc.ExpectPrepare.Query) {
				t.Fatalf("query = %#v want %#v", gotQuery, tc.ExpectPrepare.Query)
			}
			if tc.ExpectPrepare.Body == nil {
				if body != nil {
					t.Fatalf("body = %q want nil", body)
				}
				return
			}
			if body == nil {
				t.Fatal("body is nil")
			}
			if !kernelJSONEqual(body, *tc.ExpectPrepare.Body) {
				t.Fatalf("body = %s want %s", body, *tc.ExpectPrepare.Body)
			}
		})
	}
}

func TestDecodeCasesFromFixture(t *testing.T) {
	t.Parallel()
	for _, tc := range loadTransportKernelCases(t) {
		if tc.ExpectDecode == nil || tc.RawResponse == nil {
			continue
		}
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			response := &protov1.OperationResult{}
			if err := DecodeRESTResponse(MethodAppInvoke, response, rawResponseFromFixture(tc.RawResponse)); err != nil {
				t.Fatalf("DecodeRESTResponse: %v", err)
			}
			if int(response.GetStatus()) != tc.ExpectDecode.Status {
				t.Fatalf("status = %d want %d", response.GetStatus(), tc.ExpectDecode.Status)
			}
			wantBody, err := base64.StdEncoding.DecodeString(tc.ExpectDecode.BodyBase64)
			if err != nil {
				t.Fatalf("decode expected body: %v", err)
			}
			if string(response.GetBody()) != string(wantBody) {
				t.Fatalf("body = %q want %q", response.GetBody(), wantBody)
			}
			for _, key := range tc.ExpectDecode.HeaderKeys {
				if response.GetHeaders()[key] == nil {
					t.Fatalf("missing header %q", key)
				}
			}
			for key, wantCount := range tc.ExpectDecode.HeaderValueCounts {
				got := response.GetHeaders()[key]
				if got == nil || len(got.GetValues()) != wantCount {
					t.Fatalf("header %q count = %d want %d", key, len(got.GetValues()), wantCount)
				}
			}
		})
	}
}

func TestGatewayCasesFromFixture(t *testing.T) {
	t.Parallel()
	for _, tc := range loadTransportKernelCases(t) {
		if tc.ExpectGatewayError == nil || tc.RawResponse == nil {
			continue
		}
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			err := ParseGatewayError(tc.RawResponse.Status, rawBodyFromFixture(tc.RawResponse))
			if err == nil {
				t.Fatal("expected error")
			}
			if int(err.Code) != tc.ExpectGatewayError.Code {
				t.Fatalf("code = %v want %v", err.Code, tc.ExpectGatewayError.Code)
			}
			if tc.ExpectGatewayError.Message != "" && err.Message != tc.ExpectGatewayError.Message {
				t.Fatalf("message = %q want %q", err.Message, tc.ExpectGatewayError.Message)
			}
		})
	}
}

func TestGestaltErrorCasesFromFixture(t *testing.T) {
	t.Parallel()
	for _, tc := range loadTransportKernelCases(t) {
		if tc.ExpectGestaltError == nil || tc.RawResponse == nil {
			continue
		}
		tc := tc
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			response := &protov1.OperationResult{}
			err := DecodeRESTResponse(MethodAppInvoke, response, rawResponseFromFixture(tc.RawResponse))
			if err == nil {
				t.Fatal("expected error")
			}
			ge, ok := err.(*GestaltError)
			if !ok {
				t.Fatalf("expected GestaltError, got %T", err)
			}
			if int(ge.Code) != tc.ExpectGestaltError.Code {
				t.Fatalf("code = %v want %v", ge.Code, tc.ExpectGestaltError.Code)
			}
		})
	}
}

func methodForPrepareCase(base Method, tc transportKernelCase) Method {
	method := base
	if len(tc.OverrideQueryFields) > 0 {
		extra := make([]PublicField, len(tc.OverrideQueryFields))
		for i, field := range tc.OverrideQueryFields {
			extra[i] = PublicField{Name: field.Name, JSONName: field.JSONName}
		}
		method.HTTPQueryFields = append(append([]PublicField(nil), method.HTTPQueryFields...), extra...)
	}
	if tc.OverrideHttpBody != nil {
		method.HTTPBody = *tc.OverrideHttpBody
	}
	return method
}

func kernelJSONEqual(got json.RawMessage, want json.RawMessage) bool {
	var gotValue any
	var wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		return false
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		return false
	}
	return reflect.DeepEqual(gotValue, wantValue)
}

func rawResponseFromFixture(raw *struct {
	Status     int        `json:"status"`
	BodyText   string     `json:"bodyText"`
	Headers    [][]string `json:"headers"`
	BodyBase64 string     `json:"bodyBase64"`
}) RawRESTResponse {
	return RawRESTResponse{
		Status:  raw.Status,
		Headers: headersFromFixture(raw.Headers),
		Body:    rawBodyFromFixture(raw),
	}
}

func rawBodyFromFixture(raw *struct {
	Status     int        `json:"status"`
	BodyText   string     `json:"bodyText"`
	Headers    [][]string `json:"headers"`
	BodyBase64 string     `json:"bodyBase64"`
}) []byte {
	if raw.BodyText != "" {
		return []byte(raw.BodyText)
	}
	if raw.BodyBase64 != "" {
		body, err := base64.StdEncoding.DecodeString(raw.BodyBase64)
		if err == nil {
			return body
		}
	}
	return nil
}

func headersFromFixture(headers [][]string) []Header {
	out := make([]Header, 0, len(headers))
	for _, pair := range headers {
		if len(pair) != 2 {
			continue
		}
		out = append(out, Header{Name: pair[0], Value: pair[1]})
	}
	return out
}
