package httpbinding

import (
	"fmt"
	"net/http"
	"strings"
)

func ValidatePayloadTemplate(template, timestampHeader string) error {
	template = strings.TrimSpace(template)
	if template == "" {
		return fmt.Errorf("must not be empty")
	}

	requireTimestamp := strings.TrimSpace(timestampHeader) != ""
	sawTimestamp := !requireTimestamp
	err := visitPayloadTemplate(template, func(string) error {
		return nil
	}, func(token string) error {
		switch {
		case token == "raw_body":
			return nil
		case token == "request_target":
			return nil
		case strings.HasPrefix(strings.ToLower(token), "header:"):
			headerName := strings.TrimSpace(token[len("header:"):])
			if headerName == "" {
				return fmt.Errorf("contains an empty header placeholder")
			}
			if requireTimestamp && strings.EqualFold(headerName, timestampHeader) {
				sawTimestamp = true
			}
			return nil
		default:
			return fmt.Errorf("placeholder %q is not supported", token)
		}
	})
	if err != nil {
		return err
	}
	if !sawTimestamp {
		return fmt.Errorf("must include a header placeholder for %q", timestampHeader)
	}
	return nil
}

func PayloadTemplateReferencesHeader(template, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	found := false
	_ = visitPayloadTemplate(strings.TrimSpace(template), func(string) error {
		return nil
	}, func(token string) error {
		if strings.HasPrefix(strings.ToLower(token), "header:") {
			headerName := strings.TrimSpace(token[len("header:"):])
			if strings.EqualFold(headerName, name) {
				found = true
			}
		}
		return nil
	})
	return found
}

func PayloadTemplateReferencesRequestTarget(template string) bool {
	found := false
	_ = visitPayloadTemplate(strings.TrimSpace(template), func(string) error {
		return nil
	}, func(token string) error {
		if token == "request_target" {
			found = true
		}
		return nil
	})
	return found
}

func RenderPayloadTemplate(template string, r *http.Request, rawBody []byte) (string, error) {
	var headers http.Header
	var requestTarget string
	if r != nil {
		headers = r.Header
		if r.URL != nil {
			requestTarget = r.URL.RequestURI()
		}
	}
	var out strings.Builder
	err := visitPayloadTemplate(strings.TrimSpace(template), func(literal string) error {
		out.WriteString(literal)
		return nil
	}, func(token string) error {
		switch {
		case token == "raw_body":
			_, _ = out.Write(rawBody)
			return nil
		case token == "request_target":
			out.WriteString(requestTarget)
			return nil
		case strings.HasPrefix(strings.ToLower(token), "header:"):
			headerName := strings.TrimSpace(token[len("header:"):])
			if headerName == "" {
				return fmt.Errorf("contains an empty header placeholder")
			}
			out.WriteString(headers.Get(headerName))
			return nil
		default:
			return fmt.Errorf("placeholder %q is not supported", token)
		}
	})
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func visitPayloadTemplate(template string, literalFn func(string) error, tokenFn func(string) error) error {
	if template == "" {
		return fmt.Errorf("must not be empty")
	}
	for i := 0; i < len(template); {
		start := strings.IndexByte(template[i:], '{')
		if start < 0 {
			return literalFn(template[i:])
		}
		start += i
		end := strings.IndexByte(template[start+1:], '}')
		if end < 0 {
			return fmt.Errorf("contains an unterminated placeholder")
		}
		end += start + 1
		if err := literalFn(template[i:start]); err != nil {
			return err
		}
		token := strings.TrimSpace(template[start+1 : end])
		if err := tokenFn(token); err != nil {
			return err
		}
		i = end + 1
	}
	return nil
}
