package apiexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/valon-technologies/gestalt/core"
)

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// ResponseChecker validates a response body beyond the default HTTP status check.
// If it returns a non-nil error, Do treats the response as a failure.
type ResponseChecker func(status int, body []byte) error

// Request describes an API call to make on behalf of an integration.
type Request struct {
	Method  string
	BaseURL string
	Path    string
	Params  map[string]any
	Token   string

	// AuthHeader overrides the Authorization header value.
	// Default (when empty): "Bearer {Token}".
	// If both AuthHeader and Token are empty, no Authorization header is set.
	AuthHeader string

	// CustomHeaders are extra headers set on every request.
	CustomHeaders map[string]string

	// ContentType overrides the Content-Type for requests with a body.
	// Default: "application/json".
	ContentType string

	// Body overrides the request body entirely.
	// When set, Params are not marshaled into the body but are still used for
	// path parameter substitution and query strings on GET/DELETE.
	Body []byte

	// CheckResponse, when set, replaces the default status >= 400 check.
	CheckResponse ResponseChecker
}

// Do executes the request and returns an OperationResult.
func Do(ctx context.Context, client *http.Client, req Request) (*core.OperationResult, error) {
	params := copyParams(req.Params)

	path, err := substitutePath(req.Path, params)
	if err != nil {
		return nil, err
	}

	fullURL := req.BaseURL + path

	var httpReq *http.Request

	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		var body []byte
		if req.Body != nil {
			body = req.Body
		} else {
			body, err = json.Marshal(params)
			if err != nil {
				return nil, fmt.Errorf("marshaling request body: %w", err)
			}
		}
		httpReq, err = http.NewRequestWithContext(ctx, req.Method, fullURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		ct := req.ContentType
		if ct == "" {
			ct = "application/json"
		}
		httpReq.Header.Set("Content-Type", ct)
	default:
		httpReq, err = http.NewRequestWithContext(ctx, req.Method, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		if len(params) > 0 {
			q := httpReq.URL.Query()
			for k, v := range params {
				q.Set(k, fmt.Sprintf("%v", v))
			}
			httpReq.URL.RawQuery = q.Encode()
		}
	}

	if req.AuthHeader != "" {
		httpReq.Header.Set("Authorization", req.AuthHeader)
	} else if req.Token != "" {
		httpReq.Header.Set("Authorization", core.BearerScheme+req.Token)
	}

	for k, v := range req.CustomHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if req.CheckResponse != nil {
		if err := req.CheckResponse(resp.StatusCode, respBody); err != nil {
			return nil, err
		}
	} else if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	return &core.OperationResult{
		Status: resp.StatusCode,
		Body:   string(respBody),
	}, nil
}

// GraphQLRequest describes a GraphQL API call.
type GraphQLRequest struct {
	URL           string
	Query         string
	Variables     map[string]any
	Token         string
	AuthHeader    string
	CustomHeaders map[string]string
}

const (
	graphqlBodyKeyQuery     = "query"
	graphqlBodyKeyVariables = "variables"
	graphqlRespKeyData      = "data"
	graphqlRespKeyErrors    = "errors"
)

type graphqlError struct {
	Message string `json:"message"`
}

// DoGraphQL executes a GraphQL operation. The query is sent as a JSON POST body
// with the variables. If the response contains errors, they are returned as an error.
func DoGraphQL(ctx context.Context, client *http.Client, req GraphQLRequest) (*core.OperationResult, error) {
	payload := map[string]any{
		graphqlBodyKeyQuery: req.Query,
	}
	if len(req.Variables) > 0 {
		payload[graphqlBodyKeyVariables] = req.Variables
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling graphql body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating graphql request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if req.AuthHeader != "" {
		httpReq.Header.Set("Authorization", req.AuthHeader)
	} else if req.Token != "" {
		httpReq.Header.Set("Authorization", core.BearerScheme+req.Token)
	}

	for k, v := range req.CustomHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("executing graphql request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading graphql response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, respBody)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parsing graphql response: %w", err)
	}

	if raw, ok := parsed[graphqlRespKeyErrors]; ok {
		var gqlErrs []graphqlError
		if err := json.Unmarshal(raw, &gqlErrs); err == nil && len(gqlErrs) > 0 {
			msgs := make([]string, len(gqlErrs))
			for i, e := range gqlErrs {
				msgs[i] = e.Message
			}
			return nil, fmt.Errorf("graphql: %s", strings.Join(msgs, "; "))
		}
	}

	resultBody := string(respBody)
	if raw, ok := parsed[graphqlRespKeyData]; ok {
		resultBody = string(raw)
	}

	return &core.OperationResult{
		Status: resp.StatusCode,
		Body:   resultBody,
	}, nil
}

// ParseJSONToken extracts fields from a JSON-encoded token string.
func ParseJSONToken(token string, dest any) error {
	if err := json.Unmarshal([]byte(token), dest); err != nil {
		return fmt.Errorf("parsing token: %w", err)
	}
	return nil
}

func copyParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	cp := make(map[string]any, len(params))
	for k, v := range params {
		cp[k] = v
	}
	return cp
}

func substitutePath(path string, params map[string]any) (string, error) {
	var missingErr error
	result := pathParamRe.ReplaceAllStringFunc(path, func(match string) string {
		key := match[1 : len(match)-1]
		v, ok := params[key]
		if !ok {
			missingErr = fmt.Errorf("missing required path parameter: %s", key)
			return match
		}
		delete(params, key)
		return fmt.Sprintf("%v", v)
	})
	if missingErr != nil {
		return "", missingErr
	}
	return result, nil
}
