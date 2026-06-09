package server

import (
	"io"
	"net/http"
	"strings"

	proto "github.com/valon-technologies/gestalt/server/rpc/protov1/v1"
	"google.golang.org/protobuf/encoding/protojson"
	gproto "google.golang.org/protobuf/proto"
)

func (s *Server) checkAuthorizationAccess(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthorizationProvider(w) {
		return
	}
	var req proto.CheckAccessRequest
	if !decodeProtoJSONBody(w, r, &req) {
		return
	}
	resp, err := s.authorization.CheckAccess(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *Server) requireAuthorizationProvider(w http.ResponseWriter) bool {
	if s.authorization == nil {
		writeError(w, http.StatusPreconditionFailed, "authorization provider is not configured")
		return false
	}
	return true
}

func decodeProtoJSONBody(w http.ResponseWriter, r *http.Request, msg gproto.Message) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeError(w, http.StatusBadRequest, "request body is required")
		return false
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(body, msg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeProtoJSON(w http.ResponseWriter, status int, msg gproto.Message) {
	body, err := (protojson.MarshalOptions{EmitUnpopulated: false}).Marshal(msg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
}
