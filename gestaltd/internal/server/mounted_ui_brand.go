package server

import (
	"bytes"
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
)

const mountedUIBrandJSONPath = "/brand.json"

const platformBrandScriptID = "gestalt-platform-brand"

type platformBrandPayload struct {
	Name    string `json:"name,omitempty"`
	MarkSrc string `json:"markSrc,omitempty"`
}

func absoluteBrandMarkSrc(mountPath, markSrc string) string {
	markSrc = strings.TrimSpace(markSrc)
	if markSrc == "" {
		return ""
	}
	if strings.HasPrefix(markSrc, "/") || strings.HasPrefix(markSrc, "http://") || strings.HasPrefix(markSrc, "https://") {
		return markSrc
	}
	mountPath = strings.TrimRight(strings.TrimSpace(mountPath), "/")
	if mountPath == "" {
		return "/" + markSrc
	}
	return mountPath + "/" + markSrc
}

func mountedUIBrandJSON(mounted MountedUI) []byte {
	payload := platformBrandPayload{
		Name:    strings.TrimSpace(mounted.BrandName),
		MarkSrc: absoluteBrandMarkSrc(mounted.Path, mounted.BrandMarkSrc),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return []byte("{}")
	}
	if payload.Name == "" && payload.MarkSrc == "" {
		return []byte("{}")
	}
	return body
}

func mountedUIBrandPaths(mountPath string) string {
	mountPath = strings.TrimRight(mountPath, "/")
	if mountPath == "" {
		return mountedUIBrandJSONPath
	}
	return mountPath + mountedUIBrandJSONPath
}

func serveMountedUIBrandJSON(w http.ResponseWriter, r *http.Request, mounted MountedUI) {
	serveMountedUIThemeContent(w, r, "application/json; charset=utf-8", mountedUIBrandJSON(mounted))
}

// injectPlatformBrand rewrites the frozen index.html placeholder so first paint
// sees the deployment product name and mark (no Gestalt→tenant flash).
func injectPlatformBrand(mounted MountedUI) func([]byte) []byte {
	body := mountedUIBrandJSON(mounted)
	name := strings.TrimSpace(mounted.BrandName)
	markSrc := absoluteBrandMarkSrc(mounted.Path, mounted.BrandMarkSrc)
	return func(data []byte) []byte {
		data = replacePlatformBrandScriptJSON(data, body)
		if name != "" {
			data = replaceHTMLTitle(data, name)
		}
		if markSrc != "" {
			data = replaceBrandIconHrefs(data, markSrc)
		}
		return data
	}
}

var (
	platformBrandScriptPattern = regexp.MustCompile(
		`(?is)(<script\b[^>]*\bid\s*=\s*["']` + platformBrandScriptID + `["'][^>]*>)(.*?)(</script>)`,
	)
	htmlTitlePattern     = regexp.MustCompile(`(?is)(<title\b[^>]*>)(.*?)(</title>)`)
	brandIconLinkPattern = regexp.MustCompile(
		`(?is)<link\b[^>]*\brel\s*=\s*["'](?:apple-touch-icon|shortcut icon|icon)["'][^>]*>`,
	)
	hrefAttrPattern  = regexp.MustCompile(`(?i)\bhref\s*=\s*["'][^"']*["']`)
	typeAttrPattern  = regexp.MustCompile(`(?i)\btype\s*=\s*["'][^"']*["']`)
	sizesAttrPattern = regexp.MustCompile(`(?i)\s*\bsizes\s*=\s*["'][^"']*["']`)
	relIconPattern   = regexp.MustCompile(`(?i)\brel\s*=\s*["'](?:shortcut icon|icon)["']`)
)

func replaceBrandIconHrefs(data []byte, markSrc string) []byte {
	markSrc = strings.TrimSpace(markSrc)
	if markSrc == "" {
		return data
	}
	href := []byte(`href="` + html.EscapeString(markSrc) + `"`)
	return brandIconLinkPattern.ReplaceAllFunc(data, func(tag []byte) []byte {
		if hrefAttrPattern.Match(tag) {
			tag = hrefAttrPattern.ReplaceAll(tag, href)
		}
		if relIconPattern.Match(tag) {
			if typeAttrPattern.Match(tag) {
				tag = typeAttrPattern.ReplaceAll(tag, []byte(`type="image/svg+xml"`))
			}
			tag = sizesAttrPattern.ReplaceAll(tag, nil)
		}
		return tag
	})
}

func replacePlatformBrandScriptJSON(data, body []byte) []byte {
	if platformBrandScriptPattern.Match(data) {
		return platformBrandScriptPattern.ReplaceAllFunc(data, func(match []byte) []byte {
			parts := platformBrandScriptPattern.FindSubmatch(match)
			if len(parts) != 4 {
				return match
			}
			out := make([]byte, 0, len(parts[1])+len(body)+len(parts[3]))
			out = append(out, parts[1]...)
			out = append(out, body...)
			out = append(out, parts[3]...)
			return out
		})
	}
	// Bundle without the placeholder: insert after <head>.
	tag := []byte(
		`<script type="application/json" id="` + platformBrandScriptID + `">` +
			string(body) +
			`</script>`,
	)
	lower := bytes.ToLower(data)
	if idx := bytes.Index(lower, []byte("<head>")); idx >= 0 {
		insertAt := idx + len("<head>")
		out := make([]byte, 0, len(data)+len(tag))
		out = append(out, data[:insertAt]...)
		out = append(out, tag...)
		out = append(out, data[insertAt:]...)
		return out
	}
	return data
}

func replaceHTMLTitle(data []byte, title string) []byte {
	escaped := []byte(html.EscapeString(title))
	if !htmlTitlePattern.Match(data) {
		return data
	}
	return htmlTitlePattern.ReplaceAllFunc(data, func(match []byte) []byte {
		parts := htmlTitlePattern.FindSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		out := make([]byte, 0, len(parts[1])+len(escaped)+len(parts[3]))
		out = append(out, parts[1]...)
		out = append(out, escaped...)
		out = append(out, parts[3]...)
		return out
	})
}

func composeIndexRenderers(fns ...func([]byte) []byte) func([]byte) []byte {
	return func(data []byte) []byte {
		for _, fn := range fns {
			if fn == nil {
				continue
			}
			data = fn(data)
		}
		return data
	}
}
