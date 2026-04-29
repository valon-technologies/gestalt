package providerhost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	proto "github.com/valon-technologies/gestalt/sdk/go/gen/v1"
	cryptoutil "github.com/valon-technologies/gestalt/server/core/crypto"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	S3ObjectAccessPathPrefix = "/api/v1/s3/object-access/"

	s3ObjectAccessAudience   = "gestalt-s3-object-access"
	s3ObjectAccessVersion    = 1
	defaultS3ObjectAccessTTL = 15 * time.Minute
	maxS3ObjectAccessTTL     = 24 * time.Hour
)

type S3ObjectAccessURLManager struct {
	encryptor  *cryptoutil.AESGCMEncryptor
	baseURL    string
	now        func() time.Time
	defaultTTL time.Duration
	maxTTL     time.Duration
}

type S3ObjectAccessURLRequest struct {
	PluginName         string
	BindingName        string
	Ref                s3store.ObjectRef
	Method             s3store.PresignMethod
	Expires            time.Duration
	ContentType        string
	ContentDisposition string
	Headers            map[string]string
}

type S3ObjectAccessTarget struct {
	PluginName         string
	BindingName        string
	Ref                s3store.ObjectRef
	Method             s3store.PresignMethod
	ExpiresAt          time.Time
	ContentType        string
	ContentDisposition string
	Headers            map[string]string
}

type s3ObjectAccessURLClaims struct {
	Version            int               `json:"v"`
	Audience           string            `json:"aud"`
	PluginName         string            `json:"plugin"`
	BindingName        string            `json:"binding"`
	Bucket             string            `json:"bucket"`
	Key                string            `json:"key"`
	VersionID          string            `json:"version_id,omitempty"`
	Method             string            `json:"method"`
	ExpiresAt          int64             `json:"exp"`
	ContentType        string            `json:"content_type,omitempty"`
	ContentDisposition string            `json:"content_disposition,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
}

func NewS3ObjectAccessURLManager(secret []byte, baseURL string) (*S3ObjectAccessURLManager, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("s3 object access secret is required")
	}
	encryptor, err := cryptoutil.NewAESGCM(secret)
	if err != nil {
		return nil, err
	}
	return &S3ObjectAccessURLManager{
		encryptor:  encryptor,
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		now:        time.Now,
		defaultTTL: defaultS3ObjectAccessTTL,
		maxTTL:     maxS3ObjectAccessTTL,
	}, nil
}

func (m *S3ObjectAccessURLManager) MintURL(req S3ObjectAccessURLRequest) (s3store.PresignResult, error) {
	if m == nil {
		return s3store.PresignResult{}, fmt.Errorf("s3 object access URLs are not available")
	}
	if m.baseURL == "" {
		return s3store.PresignResult{}, fmt.Errorf("server.base_url is required for s3 object access URLs")
	}
	target, err := normalizeS3ObjectAccessRequest(req, m.now().Add(m.tokenTTL(req.Expires)))
	if err != nil {
		return s3store.PresignResult{}, err
	}
	claims := s3ObjectAccessURLClaims{
		Version:            s3ObjectAccessVersion,
		Audience:           s3ObjectAccessAudience,
		PluginName:         target.PluginName,
		BindingName:        target.BindingName,
		Bucket:             target.Ref.Bucket,
		Key:                target.Ref.Key,
		VersionID:          target.Ref.VersionID,
		Method:             string(target.Method),
		ExpiresAt:          target.ExpiresAt.Unix(),
		ContentType:        target.ContentType,
		ContentDisposition: target.ContentDisposition,
		Headers:            s3store.CloneStringMap(target.Headers),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return s3store.PresignResult{}, err
	}
	token, err := m.encryptor.EncryptURLSafe(string(payload))
	if err != nil {
		return s3store.PresignResult{}, err
	}
	return s3store.PresignResult{
		URL:       m.baseURL + S3ObjectAccessPathPrefix + token,
		Method:    target.Method,
		ExpiresAt: target.ExpiresAt,
		Headers:   s3store.CloneStringMap(target.Headers),
	}, nil
}

func (m *S3ObjectAccessURLManager) ResolveToken(token string) (S3ObjectAccessTarget, error) {
	if m == nil {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object access URLs are not available")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object access token is required")
	}
	plaintext, err := m.encryptor.DecryptURLSafe(token)
	if err != nil {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object access token is invalid or expired")
	}
	var claims s3ObjectAccessURLClaims
	if err := json.Unmarshal([]byte(plaintext), &claims); err != nil {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object access token is invalid or expired")
	}
	if claims.Version != s3ObjectAccessVersion || claims.Audience != s3ObjectAccessAudience {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object access token is invalid or expired")
	}
	expiresAt := time.Unix(claims.ExpiresAt, 0).UTC()
	if !m.now().Before(expiresAt) {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object access token is invalid or expired")
	}
	return normalizeS3ObjectAccessRequest(S3ObjectAccessURLRequest{
		PluginName:         claims.PluginName,
		BindingName:        claims.BindingName,
		Ref:                s3store.ObjectRef{Bucket: claims.Bucket, Key: claims.Key, VersionID: claims.VersionID},
		Method:             s3store.PresignMethod(claims.Method),
		ContentType:        claims.ContentType,
		ContentDisposition: claims.ContentDisposition,
		Headers:            claims.Headers,
	}, expiresAt)
}

func (m *S3ObjectAccessURLManager) tokenTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return m.defaultTTL
	}
	if ttl > m.maxTTL {
		return m.maxTTL
	}
	return ttl
}

func S3PluginObjectKey(pluginName, key string) string {
	return s3NamespacePrefix(pluginName) + key
}

func normalizeS3ObjectAccessRequest(req S3ObjectAccessURLRequest, expiresAt time.Time) (S3ObjectAccessTarget, error) {
	pluginName := strings.TrimSpace(req.PluginName)
	if pluginName == "" {
		return S3ObjectAccessTarget{}, fmt.Errorf("plugin name is required")
	}
	bindingName := strings.TrimSpace(req.BindingName)
	if bindingName == "" {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 binding name is required")
	}
	ref := req.Ref
	ref.Bucket = strings.TrimSpace(ref.Bucket)
	if ref.Bucket == "" {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object bucket is required")
	}
	if ref.Key == "" {
		return S3ObjectAccessTarget{}, fmt.Errorf("s3 object key is required")
	}
	method := normalizeS3ObjectAccessMethod(req.Method)
	if method == "" {
		return S3ObjectAccessTarget{}, fmt.Errorf("unsupported s3 object access method %q", req.Method)
	}
	return S3ObjectAccessTarget{
		PluginName:         pluginName,
		BindingName:        bindingName,
		Ref:                ref,
		Method:             method,
		ExpiresAt:          expiresAt.UTC(),
		ContentType:        strings.TrimSpace(req.ContentType),
		ContentDisposition: strings.TrimSpace(req.ContentDisposition),
		Headers:            s3store.CloneStringMap(req.Headers),
	}, nil
}

func normalizeS3ObjectAccessMethod(method s3store.PresignMethod) s3store.PresignMethod {
	switch method {
	case "", s3store.PresignMethodGet:
		return s3store.PresignMethodGet
	case s3store.PresignMethodPut:
		return s3store.PresignMethodPut
	case s3store.PresignMethodDelete:
		return s3store.PresignMethodDelete
	case s3store.PresignMethodHead:
		return s3store.PresignMethodHead
	default:
		return ""
	}
}

type s3ObjectAccessServer struct {
	proto.UnimplementedS3ObjectAccessServer
	manager     *S3ObjectAccessURLManager
	pluginName  string
	bindingName string
}

func NewS3ObjectAccessServer(manager *S3ObjectAccessURLManager, pluginName, bindingName string) proto.S3ObjectAccessServer {
	return &s3ObjectAccessServer{
		manager:     manager,
		pluginName:  strings.TrimSpace(pluginName),
		bindingName: strings.TrimSpace(bindingName),
	}
}

func (s *s3ObjectAccessServer) CreateObjectAccessURL(_ context.Context, req *proto.CreateObjectAccessURLRequest) (*proto.CreateObjectAccessURLResponse, error) {
	if s.manager == nil {
		return nil, status.Error(codes.FailedPrecondition, "s3 object access URLs are not available")
	}
	result, err := s.manager.MintURL(S3ObjectAccessURLRequest{
		PluginName:         s.pluginName,
		BindingName:        s.bindingName,
		Ref:                objectRefFromProto(req.GetRef()),
		Method:             presignMethodFromProto(req.GetMethod()),
		Expires:            timeDurationSeconds(req.GetExpiresSeconds()),
		ContentType:        req.GetContentType(),
		ContentDisposition: req.GetContentDisposition(),
		Headers:            s3store.CloneStringMap(req.GetHeaders()),
	})
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return s3ObjectAccessResultToProto(result), nil
}

func s3ObjectAccessResultToProto(result s3store.PresignResult) *proto.CreateObjectAccessURLResponse {
	resp := &proto.CreateObjectAccessURLResponse{
		Url:     result.URL,
		Method:  presignMethodToProto(result.Method),
		Headers: s3store.CloneStringMap(result.Headers),
	}
	if !result.ExpiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(result.ExpiresAt)
	}
	return resp
}
