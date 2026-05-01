package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	s3store "github.com/valon-technologies/gestalt/server/core/s3"
	"github.com/valon-technologies/gestalt/server/services/s3"
)

func (s *Server) mountS3ObjectAccessRoutes(r chi.Router) {
	path := s3.ObjectAccessPathPrefix + "{token}"
	r.MethodFunc(http.MethodGet, path, s.handleS3ObjectAccess)
	r.MethodFunc(http.MethodHead, path, s.handleS3ObjectAccess)
	r.MethodFunc(http.MethodPut, path, s.handleS3ObjectAccess)
	r.MethodFunc(http.MethodDelete, path, s.handleS3ObjectAccess)
}

func (s *Server) handleS3ObjectAccess(w http.ResponseWriter, r *http.Request) {
	if s.s3ObjectAccessURLs == nil {
		writeError(w, http.StatusNotFound, "s3 object access is not configured")
		return
	}
	token := chi.URLParam(r, "token")
	target, err := s.s3ObjectAccessURLs.ResolveToken(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired s3 object access URL")
		return
	}
	if r.Method != string(target.Method) {
		writeError(w, http.StatusMethodNotAllowed, "s3 object access URL was created for a different method")
		return
	}
	client := s.s3[target.BindingName]
	if client == nil {
		writeError(w, http.StatusNotFound, "s3 binding not found")
		return
	}
	if err := enforceS3ObjectAccessHeaders(r, target, target.Method == s3store.PresignMethodPut); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref := target.Ref
	ref.Key = s3.PluginObjectKey(target.PluginName, ref.Key)

	switch target.Method {
	case s3store.PresignMethodGet:
		s.handleS3ObjectAccessGet(w, r, client, target, ref)
	case s3store.PresignMethodHead:
		s.handleS3ObjectAccessHead(w, r, client, target, ref)
	case s3store.PresignMethodPut:
		allowRequestBodyBytes(r, s3ObjectAccessMaxBodyBytes)
		s.handleS3ObjectAccessPut(w, r, client, target, ref)
	case s3store.PresignMethodDelete:
		s.handleS3ObjectAccessDelete(w, r, client, ref)
	default:
		writeError(w, http.StatusMethodNotAllowed, "unsupported s3 object access method")
	}
}

func (s *Server) handleS3ObjectAccessGet(w http.ResponseWriter, r *http.Request, client s3store.Client, target s3.ObjectAccessTarget, ref s3store.ObjectRef) {
	readReq, partial, err := s3ObjectAccessReadRequest(r, client, ref)
	if err != nil {
		writeS3ObjectAccessError(w, err)
		return
	}
	result, err := client.ReadObject(r.Context(), readReq)
	if err != nil {
		writeS3ObjectAccessError(w, err)
		return
	}
	defer func() { _ = result.Body.Close() }()
	writeS3ObjectAccessMetaHeaders(w, result.Meta, target)
	if partial.CoversFullRepresentation(result.Meta.Size) {
		partial = s3HTTPRange{}
	}
	status := http.StatusOK
	if partial.Requested {
		status = http.StatusPartialContent
		contentLength := partial.ContentLength(result.Meta.Size)
		w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
		w.Header().Set("Content-Range", partial.ContentRange(result.Meta.Size))
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, result.Body)
}

func (s *Server) handleS3ObjectAccessHead(w http.ResponseWriter, r *http.Request, client s3store.Client, target s3.ObjectAccessTarget, ref s3store.ObjectRef) {
	meta, err := client.HeadObject(r.Context(), ref)
	if err != nil {
		writeS3ObjectAccessError(w, err)
		return
	}
	writeS3ObjectAccessMetaHeaders(w, meta, target)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleS3ObjectAccessPut(w http.ResponseWriter, r *http.Request, client s3store.Client, target s3.ObjectAccessTarget, ref s3store.ObjectRef) {
	contentType := target.ContentType
	if contentType == "" {
		contentType = r.Header.Get("Content-Type")
	}
	meta, err := client.WriteObject(r.Context(), s3store.WriteRequest{
		Ref:                ref,
		ContentType:        contentType,
		ContentDisposition: target.ContentDisposition,
		IfMatch:            r.Header.Get("If-Match"),
		IfNoneMatch:        r.Header.Get("If-None-Match"),
		Body:               r.Body,
	})
	if err != nil {
		writeS3ObjectAccessError(w, err)
		return
	}
	if meta.ETag != "" {
		w.Header().Set("ETag", meta.ETag)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"etag": meta.ETag,
		"size": meta.Size,
	})
}

func (s *Server) handleS3ObjectAccessDelete(w http.ResponseWriter, r *http.Request, client s3store.Client, ref s3store.ObjectRef) {
	if err := client.DeleteObject(r.Context(), ref); err != nil {
		writeS3ObjectAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func enforceS3ObjectAccessHeaders(r *http.Request, target s3.ObjectAccessTarget, enforceContentType bool) error {
	if enforceContentType && target.ContentType != "" && r.Header.Get("Content-Type") != target.ContentType {
		return fmt.Errorf("Content-Type header must be %q", target.ContentType)
	}
	for name, want := range target.Headers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.EqualFold(name, "content-length") {
			got := r.ContentLength
			if got < 0 {
				return fmt.Errorf("Content-Length header is required")
			}
			wantBytes, err := strconv.ParseInt(want, 10, 64)
			if err != nil || wantBytes < 0 {
				return fmt.Errorf("invalid required Content-Length %q", want)
			}
			if got != wantBytes {
				return fmt.Errorf("Content-Length header must be %d", wantBytes)
			}
			continue
		}
		if got := r.Header.Get(name); got != want {
			return fmt.Errorf("%s header must be %q", http.CanonicalHeaderKey(name), want)
		}
	}
	return nil
}

type s3HTTPRange struct {
	Requested bool
	Start     *int64
	End       *int64
}

func (r s3HTTPRange) ContentLength(size int64) int64 {
	if !r.Requested || size <= 0 {
		return 0
	}
	start := int64(0)
	if r.Start != nil {
		start = *r.Start
	}
	end := size - 1
	if r.End != nil && *r.End < end {
		end = *r.End
	}
	if start > end {
		return 0
	}
	return end - start + 1
}

func (r s3HTTPRange) ContentRange(size int64) string {
	if size <= 0 {
		return "bytes */0"
	}
	start := int64(0)
	if r.Start != nil {
		start = *r.Start
	}
	end := size - 1
	if r.End != nil && *r.End < end {
		end = *r.End
	}
	return fmt.Sprintf("bytes %d-%d/%d", start, end, size)
}

func (r s3HTTPRange) CoversFullRepresentation(size int64) bool {
	if !r.Requested || size <= 0 {
		return false
	}
	start := int64(0)
	if r.Start != nil {
		start = *r.Start
	}
	end := size - 1
	if r.End != nil && *r.End < end {
		end = *r.End
	}
	return start == 0 && end == size-1
}

var errS3ObjectAccessInvalidConditionalHeader = errors.New("s3 object access conditional header is invalid")

func s3ObjectAccessReadRequest(r *http.Request, client s3store.Client, ref s3store.ObjectRef) (s3store.ReadRequest, s3HTTPRange, error) {
	out := s3store.ReadRequest{
		Ref:         ref,
		IfMatch:     r.Header.Get("If-Match"),
		IfNoneMatch: r.Header.Get("If-None-Match"),
	}
	if value := r.Header.Get("If-Modified-Since"); value != "" {
		t, err := http.ParseTime(value)
		if err != nil {
			return s3store.ReadRequest{}, s3HTTPRange{}, fmt.Errorf("%w: If-Modified-Since", errS3ObjectAccessInvalidConditionalHeader)
		}
		out.IfModifiedSince = &t
	}
	if value := r.Header.Get("If-Unmodified-Since"); value != "" {
		t, err := http.ParseTime(value)
		if err != nil {
			return s3store.ReadRequest{}, s3HTTPRange{}, fmt.Errorf("%w: If-Unmodified-Since", errS3ObjectAccessInvalidConditionalHeader)
		}
		out.IfUnmodifiedSince = &t
	}
	byteRange, httpRange, err := parseS3ObjectAccessHTTPRange(r.Header.Get("Range"))
	if err != nil {
		return s3store.ReadRequest{}, s3HTTPRange{}, err
	}
	if byteRange != nil && byteRange.Start == nil && byteRange.End != nil {
		meta, err := client.HeadObject(r.Context(), ref)
		if err != nil {
			return s3store.ReadRequest{}, s3HTTPRange{}, err
		}
		suffix := *byteRange.End
		if suffix <= 0 || meta.Size <= 0 {
			return s3store.ReadRequest{}, s3HTTPRange{}, s3store.ErrInvalidRange
		}
		start := meta.Size - suffix
		if start < 0 {
			start = 0
		}
		end := meta.Size - 1
		byteRange.Start = &start
		byteRange.End = &end
		httpRange.Start = &start
		httpRange.End = &end
	}
	out.Range = byteRange
	return out, httpRange, nil
}

func parseS3ObjectAccessHTTPRange(value string) (*s3store.ByteRange, s3HTTPRange, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, s3HTTPRange{}, nil
	}
	spec, ok := strings.CutPrefix(value, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return nil, s3HTTPRange{}, s3store.ErrInvalidRange
	}
	startRaw, endRaw, ok := strings.Cut(spec, "-")
	if !ok {
		return nil, s3HTTPRange{}, s3store.ErrInvalidRange
	}
	startRaw = strings.TrimSpace(startRaw)
	endRaw = strings.TrimSpace(endRaw)
	if startRaw == "" && endRaw == "" {
		return nil, s3HTTPRange{}, s3store.ErrInvalidRange
	}
	out := &s3store.ByteRange{}
	httpRange := s3HTTPRange{Requested: true}
	if startRaw != "" {
		start, err := strconv.ParseInt(startRaw, 10, 64)
		if err != nil || start < 0 {
			return nil, s3HTTPRange{}, s3store.ErrInvalidRange
		}
		out.Start = &start
		httpRange.Start = &start
	}
	if endRaw != "" {
		end, err := strconv.ParseInt(endRaw, 10, 64)
		if err != nil || end < 0 {
			return nil, s3HTTPRange{}, s3store.ErrInvalidRange
		}
		out.End = &end
		httpRange.End = &end
	}
	if out.Start != nil && out.End != nil && *out.Start > *out.End {
		return nil, s3HTTPRange{}, s3store.ErrInvalidRange
	}
	return out, httpRange, nil
}

const s3ObjectAccessContentSecurityPolicy = "default-src 'none'; sandbox; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func writeS3ObjectAccessMetaHeaders(w http.ResponseWriter, meta s3store.ObjectMeta, target s3.ObjectAccessTarget) {
	if meta.ETag != "" {
		w.Header().Set("ETag", meta.ETag)
	}
	if meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if meta.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	}
	if !meta.LastModified.IsZero() {
		w.Header().Set("Last-Modified", meta.LastModified.UTC().Format(http.TimeFormat))
	}
	w.Header().Set("Content-Disposition", s3ObjectAccessResponseContentDisposition(target, meta))
	w.Header().Set("Content-Security-Policy", s3ObjectAccessContentSecurityPolicy)
}

func s3ObjectAccessResponseContentDisposition(target s3.ObjectAccessTarget, meta s3store.ObjectMeta) string {
	raw := strings.TrimSpace(target.ContentDisposition)
	if raw == "" {
		return "attachment"
	}
	disposition, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return "attachment"
	}
	switch strings.ToLower(disposition) {
	case "attachment":
		formatted := mime.FormatMediaType("attachment", params)
		if formatted == "" {
			return "attachment"
		}
		return formatted
	case "inline":
		if !s3ObjectAccessAllowsInline(target.ContentType, meta.ContentType) {
			return "attachment"
		}
		formatted := mime.FormatMediaType("inline", params)
		if formatted == "" {
			return "inline"
		}
		return formatted
	default:
		return "attachment"
	}
}

func s3ObjectAccessAllowsInline(targetContentType, metaContentType string) bool {
	targetMediaType, ok := s3ObjectAccessMediaType(targetContentType)
	if !ok || !safeS3ObjectAccessInlineMediaType(targetMediaType) {
		return false
	}
	metaMediaType, ok := s3ObjectAccessMediaType(metaContentType)
	if !ok {
		return false
	}
	return metaMediaType == targetMediaType
}

func s3ObjectAccessMediaType(value string) (string, bool) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil || mediaType == "" {
		return "", false
	}
	return strings.ToLower(mediaType), true
}

func safeS3ObjectAccessInlineMediaType(mediaType string) bool {
	switch mediaType {
	case "text/plain",
		"image/avif",
		"image/bmp",
		"image/gif",
		"image/jpeg",
		"image/png",
		"image/tiff",
		"image/webp":
		return true
	default:
		return strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "video/")
	}
}

func writeS3ObjectAccessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errS3ObjectAccessInvalidConditionalHeader):
		writeError(w, http.StatusBadRequest, "s3 object conditional header is invalid")
	case errors.Is(err, s3store.ErrNotFound):
		writeError(w, http.StatusNotFound, "s3 object not found")
	case errors.Is(err, s3store.ErrPreconditionFailed):
		writeError(w, http.StatusPreconditionFailed, "s3 object precondition failed")
	case errors.Is(err, s3store.ErrInvalidRange):
		writeError(w, http.StatusRequestedRangeNotSatisfiable, "s3 object range is invalid")
	default:
		writeError(w, http.StatusInternalServerError, "s3 object access failed")
	}
}
