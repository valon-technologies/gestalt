package appregistryremote

import (
	"github.com/valon-technologies/gestalt/server/internal/appregistry"
)

const sessionStatePublished = "published"
const sessionStateFailed = "failed"

// SessionUpload describes a scoped artifact upload lease returned by the API.
type SessionUpload struct {
	Platform  string            `json:"platform"`
	UploadURL string            `json:"uploadUrl"`
	ExpiresAt string            `json:"expiresAt"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// SessionResponse is the typed API contract for publish session endpoints.
type SessionResponse struct {
	PublishID         string          `json:"publishId"`
	App               string          `json:"app"`
	Registry          string          `json:"registry"`
	Version           string          `json:"version"`
	State             string          `json:"state"`
	Uploads           []SessionUpload `json:"uploads"`
	MissingUploads    []string        `json:"missingUploads"`
	MismatchedUploads []string        `json:"mismatchedUploads"`
	FailureReason     string          `json:"failureReason"`
	PublishedAt       string          `json:"publishedAt"`
	Publisher         string          `json:"publisher"`
	Renewed           bool            `json:"renewed"`
}

// CreateSessionRequest is the POST body for publish session creation.
type CreateSessionRequest struct {
	Declaration *appregistry.PublishDeclaration `json:"declaration"`
}

// PublishResult summarizes a completed or resumed remote publish.
type PublishResult struct {
	PublishID   string
	App         string
	Version     string
	State       string
	AdminURL    string
	Renewed     bool
	PublishedAt string
}

func (r SessionResponse) terminal() bool {
	return r.State == sessionStatePublished || r.State == sessionStateFailed
}

func (r SessionResponse) uploadByPlatform() map[string]SessionUpload {
	out := make(map[string]SessionUpload, len(r.Uploads))
	for _, upload := range r.Uploads {
		out[upload.Platform] = upload
	}
	return out
}
