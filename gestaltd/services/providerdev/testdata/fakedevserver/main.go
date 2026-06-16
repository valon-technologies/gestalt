package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/websocket"
)

func main() {
	port := strings.TrimSpace(os.Getenv("GESTALT_DEV_PORT"))
	basePath := strings.TrimSpace(os.Getenv("GESTALT_DEV_BASE_PATH"))
	if port == "" || basePath == "" {
		log.Fatal("GESTALT_DEV_PORT and GESTALT_DEV_BASE_PATH are required")
	}
	if pidFile := strings.TrimSpace(os.Getenv("GESTALT_FAKE_PID_FILE")); pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = strings.TrimRight(basePath, "/")

	mux := http.NewServeMux()
	mux.HandleFunc(basePath+"/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, basePath) {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "dev-ok")
	})
	mux.Handle(basePath+"/ws", websocket.Handler(func(ws *websocket.Conn) {
		_, _ = io.Copy(ws, ws)
	}))

	addr := "127.0.0.1:" + port
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
