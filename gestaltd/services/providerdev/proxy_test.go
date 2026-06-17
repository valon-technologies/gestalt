package providerdev

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/net/websocket"
)

func TestReverseProxyWebSocketThroughH2C(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mount/ws" {
			http.NotFound(w, r)
			return
		}
		websocket.Handler(func(ws *websocket.Conn) {
			var msg string
			if err := websocket.Message.Receive(ws, &msg); err != nil {
				return
			}
			_ = websocket.Message.Send(ws, msg)
		}).ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)

	u := mustParseURL(upstream.URL)
	proxy := newReverseProxy(u)
	root := http.NewServeMux()
	root.Handle("/mount/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	}))
	handler := h2c.NewHandler(root, &http2.Server{})

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ws, err := websocket.Dial(strings.Replace(server.URL, "http://", "ws://", 1)+"/mount/ws", "", server.URL+"/mount/")
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer func() { _ = ws.Close() }()
	if err := websocket.Message.Send(ws, "ping"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var msg string
	if err := websocket.Message.Receive(ws, &msg); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if msg != "ping" {
		t.Fatalf("msg = %q, want ping", msg)
	}
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
