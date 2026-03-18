package hex

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/valon-technologies/toolshed/internal/provider"
)

func init() {
	provider.RegisterResponseChecker("hex_response", hexResponseCheck)
}

func hexResponseCheck(status int, body []byte) error {
	if status < http.StatusBadRequest {
		return nil
	}
	var resp struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Message != "" {
		return fmt.Errorf("hex API error (HTTP %d): %s", status, resp.Message)
	}
	return fmt.Errorf("HTTP %d: %s", status, body)
}
