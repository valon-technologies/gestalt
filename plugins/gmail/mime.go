package gmail

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

var b64 = base64.URLEncoding.WithPadding(base64.NoPadding)

type mimeParams struct {
	To         string
	Subject    string
	Body       string
	Cc         string
	Bcc        string
	HtmlBody   string
	InReplyTo  string
	References string
}

func sanitizeHeader(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

func buildMIME(mp mimeParams) string {
	var buf strings.Builder

	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("To: " + sanitizeHeader(mp.To) + "\r\n")
	buf.WriteString("Subject: " + sanitizeHeader(mp.Subject) + "\r\n")
	if mp.Cc != "" {
		buf.WriteString("Cc: " + sanitizeHeader(mp.Cc) + "\r\n")
	}
	if mp.Bcc != "" {
		buf.WriteString("Bcc: " + sanitizeHeader(mp.Bcc) + "\r\n")
	}
	if mp.InReplyTo != "" {
		buf.WriteString("In-Reply-To: " + sanitizeHeader(mp.InReplyTo) + "\r\n")
		buf.WriteString("References: " + sanitizeHeader(mp.References) + "\r\n")
	}

	if mp.HtmlBody != "" {
		boundary := randomBoundary()
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n", boundary))
		buf.WriteString("\r\n")
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
		buf.WriteString(mp.Body)
		buf.WriteString("\r\n--" + boundary + "\r\n")
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		buf.WriteString(mp.HtmlBody)
		buf.WriteString("\r\n--" + boundary + "--\r\n")
	} else {
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(mp.Body)
	}

	return b64.EncodeToString([]byte(buf.String()))
}

func randomBoundary() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return "gestalt_" + hex.EncodeToString(buf[:])
}

func getHeader(headers []map[string]string, name string) string {
	nameLower := strings.ToLower(name)
	for _, h := range headers {
		if strings.ToLower(h["name"]) == nameLower {
			return h["value"]
		}
	}
	return ""
}

func ensureReplyPrefix(subject string) string {
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func extractEmail(addr string) string {
	if start := strings.LastIndex(addr, "<"); start != -1 {
		if end := strings.Index(addr[start:], ">"); end != -1 {
			return addr[start+1 : start+end]
		}
	}
	return strings.TrimSpace(addr)
}

func filterSelfFromRecipients(recipients, selfEmail string) string {
	if selfEmail == "" {
		return recipients
	}
	selfLower := strings.ToLower(selfEmail)
	var filtered []string
	for _, addr := range strings.Split(recipients, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if strings.ToLower(extractEmail(addr)) == selfLower {
			continue
		}
		filtered = append(filtered, addr)
	}
	return strings.Join(filtered, ", ")
}

func ensureForwardPrefix(subject string) string {
	if strings.HasPrefix(strings.ToLower(subject), "fwd:") {
		return subject
	}
	return "Fwd: " + subject
}
