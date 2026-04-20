package apiexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/valon-technologies/gestalt/server/core"
)

var ErrMissingPathParam = errors.New("missing required path parameter")

const (
	defaultMaxRetries   = 3
	baseRetryDelay      = 1 * time.Second
	maxResponseBodySize = 50 << 20 // 50 MB
)

var retryableStatusCodes = map[int]bool{
	http.StatusTooManyRequests:    true,
	http.StatusBadGateway:         true,
	http.StatusServiceUnavailable: true,
	http.StatusGatewayTimeout:     true,
}

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

var (
	ErrUpstreamUnavailable     = errors.New("failed to reach upstream service")
	ErrUpstreamTimedOut        = errors.New("upstream service timed out")
	ErrUpstreamResponseRead    = errors.New("failed to read upstream response")
	ErrUpstreamInvalidResponse = errors.New("upstream service returned an invalid response")
)

type UpstreamHTTPError struct {
	Status            int
	Headers           http.Header
	Body              string
	Cause             error
	retryAfterSeconds int
}

func (e *UpstreamHTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

func (e *UpstreamHTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *UpstreamHTTPError) retryError() error {
	if e == nil || !retryableStatusCodes[e.Status] {
		return nil
	}
	if e.retryAfterSeconds > 0 {
		return &retryableRequestError{
			cause:             e,
			retryAfterSeconds: e.retryAfterSeconds,
		}
	}
	return e
}

type UpstreamOperationError struct {
	Message string
}

func (e *UpstreamOperationError) Error() string {
	if e == nil || e.Message == "" {
		return "upstream operation failed"
	}
	return e.Message
}

type retryableRequestError struct {
	cause             error
	retryAfterSeconds int
}

func (e *retryableRequestError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *retryableRequestError) Unwrap() []error {
	if e == nil || e.cause == nil {
		return nil
	}
	if e.retryAfterSeconds <= 0 {
		return []error{e.cause}
	}
	return []error{e.cause, backoff.RetryAfter(e.retryAfterSeconds)}
}

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

	// QueryParams are always applied as URL query parameters, regardless of
	// HTTP method. When set on a POST/PUT/PATCH, these go on the URL while
	// Params become the JSON body. When nil, the existing method-based
	// routing applies.
	QueryParams map[string]any

	// CheckResponse, when set, replaces the default status >= 400 check.
	CheckResponse ResponseChecker

	// MaxRetries is the maximum number of retry attempts for transient errors.
	// Zero uses the default (3). Set NoRetry to disable retries entirely.
	MaxRetries int
	// NoRetry disables automatic retry for this request.
	NoRetry bool
}

// Do executes the request and returns an OperationResult.
func Do(ctx context.Context, client *http.Client, req Request) (*core.OperationResult, error) {
	params := maps.Clone(req.Params)

	path, err := substitutePath(req.Path, params)
	if err != nil {
		return nil, err
	}

	fullURL := req.BaseURL + path
	if len(req.QueryParams) > 0 {
		q := url.Values{}
		for k, v := range req.QueryParams {
			addQueryValue(q, k, v)
		}
		fullURL += "?" + q.Encode()
	}

	var bodyBytes []byte
	var contentType string
	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		if req.Body != nil {
			bodyBytes = req.Body
		} else {
			bodyBytes, err = json.Marshal(params)
			if err != nil {
				return nil, fmt.Errorf("marshaling request body: %w", err)
			}
		}
		contentType = req.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
	}

	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	if req.NoRetry {
		maxRetries = 0
	}

	result, err := backoff.Retry(ctx, func() (*core.OperationResult, error) {
		result, err := doOnce(
			ctx,
			client,
			req,
			fullURL,
			bodyBytes,
			contentType,
			params,
		)
		if err == nil {
			return result, nil
		}

		var upstreamErr *UpstreamHTTPError
		if errors.As(err, &upstreamErr) {
			if retryErr := upstreamErr.retryError(); retryErr != nil {
				return nil, retryErr
			}
		}

		return nil, backoff.Permanent(err)
	},
		backoff.WithBackOff(newRetryBackOff()),
		backoff.WithMaxElapsedTime(0),
		backoff.WithMaxTries(uint(maxRetries+1)),
	)
	if err != nil {
		var permanent *backoff.PermanentError
		if errors.As(err, &permanent) {
			err = permanent.Unwrap()
		}
		return nil, err
	}
	return result, nil
}

func doOnce(
	ctx context.Context,
	client *http.Client,
	req Request,
	fullURL string,
	bodyBytes []byte,
	contentType string,
	params map[string]any,
) (*core.OperationResult, error) {
	var httpReq *http.Request
	var err error

	switch req.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		httpReq, err = http.NewRequestWithContext(ctx, req.Method, fullURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		httpReq.Header.Set("Content-Type", contentType)
	default:
		httpReq, err = http.NewRequestWithContext(ctx, req.Method, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		if len(params) > 0 {
			q := httpReq.URL.Query()
			for k, v := range params {
				addQueryValue(q, k, v)
			}
			httpReq.URL.RawQuery = q.Encode()
		}
	}

	for k, v := range req.CustomHeaders {
		httpReq.Header.Set(k, v)
	}

	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	if req.AuthHeader != "" {
		httpReq.Header.Set("Authorization", req.AuthHeader)
	} else if req.Token != "" {
		httpReq.Header.Set("Authorization", core.BearerScheme+req.Token)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %w", ErrUpstreamTimedOut, err)
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			return nil, fmt.Errorf("%w: %w", ErrUpstreamTimedOut, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	}

	retryAfterSeconds := parseRetryAfterSeconds(resp.Header.Get("Retry-After"))
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstreamResponseRead, err)
	}

	if req.CheckResponse != nil {
		if err := req.CheckResponse(resp.StatusCode, respBody); err != nil {
			if resp.StatusCode >= http.StatusBadRequest {
				return nil, &UpstreamHTTPError{
					Status:            resp.StatusCode,
					Headers:           resp.Header.Clone(),
					Body:              string(respBody),
					Cause:             err,
					retryAfterSeconds: retryAfterSeconds,
				}
			}
			return nil, err
		}
	} else if resp.StatusCode >= http.StatusBadRequest {
		return nil, &UpstreamHTTPError{
			Status:            resp.StatusCode,
			Headers:           resp.Header.Clone(),
			Body:              string(respBody),
			retryAfterSeconds: retryAfterSeconds,
		}
	}

	return &core.OperationResult{
		Status:  resp.StatusCode,
		Headers: resp.Header,
		Body:    string(respBody),
	}, nil
}

func newRetryBackOff() backoff.BackOff {
	policy := backoff.NewExponentialBackOff()
	policy.InitialInterval = baseRetryDelay
	policy.RandomizationFactor = 0
	policy.Multiplier = 2
	policy.MaxInterval = 24 * time.Hour
	return policy
}

func parseRetryAfterSeconds(value string) int {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds
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

	for k, v := range req.CustomHeaders {
		httpReq.Header.Set(k, v)
	}

	if req.AuthHeader != "" {
		httpReq.Header.Set("Authorization", req.AuthHeader)
	} else if req.Token != "" {
		httpReq.Header.Set("Authorization", core.BearerScheme+req.Token)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("%w: %w", ErrUpstreamTimedOut, err)
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			return nil, fmt.Errorf("%w: %w", ErrUpstreamTimedOut, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstreamResponseRead, err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, &UpstreamHTTPError{
			Status:  resp.StatusCode,
			Headers: resp.Header.Clone(),
			Body:    string(respBody),
		}
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUpstreamInvalidResponse, err)
	}

	if raw, ok := parsed[graphqlRespKeyErrors]; ok {
		var gqlErrs []graphqlError
		if err := json.Unmarshal(raw, &gqlErrs); err == nil && len(gqlErrs) > 0 {
			msgs := make([]string, len(gqlErrs))
			for i, e := range gqlErrs {
				msgs[i] = e.Message
			}
			return nil, &UpstreamOperationError{Message: strings.Join(msgs, "; ")}
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

func ExpandedPathWithQuery(method, path string, params map[string]any, queryParams map[string]any) string {
	params = maps.Clone(params)
	expanded, err := substitutePath(path, params)
	if err != nil {
		return path
	}
	q := url.Values{}
	for k, v := range queryParams {
		addQueryValue(q, k, v)
	}
	switch method {
	case http.MethodGet, http.MethodDelete:
		for k, v := range params {
			addQueryValue(q, k, v)
		}
	}
	if len(q) > 0 {
		return expanded + "?" + q.Encode()
	}
	return expanded
}

func addQueryValue(q url.Values, key string, value any) {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			q.Add(key, fmt.Sprintf("%v", item))
		}
	case []string:
		for _, item := range v {
			q.Add(key, item)
		}
	default:
		q.Add(key, fmt.Sprintf("%v", value))
	}
}

func substitutePath(path string, params map[string]any) (string, error) {
	var missingErr error
	result := pathParamRe.ReplaceAllStringFunc(path, func(match string) string {
		key := match[1 : len(match)-1]
		v, ok := params[key]
		if !ok {
			missingErr = fmt.Errorf("%w: %s", ErrMissingPathParam, key)
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
