package scim

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const retryAfterSeconds = 1

func paginationQueryInt(r *http.Request, name string, defaultValue, minimum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalid(name + " is invalid")
	}
	if value < minimum {
		value = minimum
	}
	return value, nil
}

func pagination(r *http.Request) (int, int, error) {
	start, err := paginationQueryInt(r, "startIndex", 1, 1)
	if err != nil {
		return 0, 0, err
	}
	count, err := paginationQueryInt(r, "count", 100, 0)
	if err != nil {
		return 0, 0, err
	}
	if count > 200 {
		count = 200
	}
	return start, count, nil
}

func decodeBody(r *http.Request, target any) error {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/scim+json" {
		return invalid("Content-Type must be application/scim+json")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		return invalid("request body is not valid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return invalid("request body must contain one JSON value")
	}
	return nil
}

func writeError(w http.ResponseWriter, err error) {
	scimErr := &Error{Status: http.StatusInternalServerError, Detail: "internal SCIM server error"}
	if !errors.As(err, &scimErr) {
		scimErr = &Error{Status: http.StatusServiceUnavailable, Detail: "SCIM service is temporarily unavailable", Retry: true}
	}
	if scimErr.Retry {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	}
	payload := map[string]any{"schemas": []string{ErrorSchemaURN}, "status": strconv.Itoa(scimErr.Status), "detail": scimErr.Detail}
	if scimErr.SCIMType != "" {
		payload["scimType"] = scimErr.SCIMType
	}
	writeJSON(w, scimErr.Status, payload)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
