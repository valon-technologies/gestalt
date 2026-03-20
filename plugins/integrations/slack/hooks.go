package slack

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/valon-technologies/gestalt/internal/provider"
)

func init() {
	provider.RegisterResponseChecker("slack_ok", slackOKCheck)
	provider.RegisterResponseHook("slack_ok", func(body []byte) error {
		return slackOKCheck(http.StatusOK, body)
	})
}

func slackOKCheck(status int, body []byte) error {
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		if status >= http.StatusBadRequest {
			return fmt.Errorf("HTTP %d: %s", status, body)
		}
		return nil
	}
	if !resp.OK {
		msg := resp.Error
		if msg == "" {
			msg = "unknown error"
		}
		return fmt.Errorf("slack API error: %s", msg)
	}
	return nil
}
