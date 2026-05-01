// Command indexeddbtransportd runs a minimal IndexedDB gRPC server for SDK
// transport tests. It prints READY to stdout when listening and serves until
// killed.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/valon-technologies/gestalt/server/internal/testutil/indexeddbtransport"
	"github.com/valon-technologies/gestalt/server/services/runtimehost"
	"golang.org/x/net/http2"
	"google.golang.org/grpc/codes"
)

func main() {
	socket := flag.String("socket", "", "Unix socket path to listen on")
	tcp := flag.String("tcp", "", "TCP host:port to listen on")
	relayTLS := flag.String("relay-tls", "", "TLS relay host:port to listen on")
	certFile := flag.String("cert-file", "", "PEM certificate for --relay-tls")
	keyFile := flag.String("key-file", "", "PEM private key for --relay-tls")
	expectToken := flag.String("expect-token", "", "Require the relay token header to match this value")
	flag.Parse()
	modeCount := 0
	if strings.TrimSpace(*socket) != "" {
		modeCount++
	}
	if strings.TrimSpace(*tcp) != "" {
		modeCount++
	}
	if strings.TrimSpace(*relayTLS) != "" {
		modeCount++
	}
	if modeCount != 1 {
		fmt.Fprintln(os.Stderr, "usage: indexeddbtransportd --socket <path> | --tcp <host:port> | --relay-tls <host:port>")
		os.Exit(1)
	}

	srv, err := startHarness(
		strings.TrimSpace(*socket),
		strings.TrimSpace(*tcp),
		strings.TrimSpace(*relayTLS),
		strings.TrimSpace(*certFile),
		strings.TrimSpace(*keyFile),
		strings.TrimSpace(*expectToken),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start: %v\n", err)
		os.Exit(1)
	}
	defer srv.Stop()

	fmt.Println("READY")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

type harness interface {
	Stop()
}

func startHarness(
	socket string,
	tcp string,
	relayTLS string,
	certFile string,
	keyFile string,
	expectToken string,
) (harness, error) {
	switch {
	case relayTLS != "":
		if certFile == "" || keyFile == "" {
			return nil, fmt.Errorf("--relay-tls requires --cert-file and --key-file")
		}
		return startTLSRelay(relayTLS, certFile, keyFile, expectToken)
	case tcp != "":
		return indexeddbtransport.Start("tcp://"+tcp, indexeddbtransport.Options{
			ExpectRelayToken: expectToken,
		})
	default:
		return indexeddbtransport.Start(socket, indexeddbtransport.Options{
			ExpectRelayToken: expectToken,
		})
	}
}

type relayHarness struct {
	backend    *indexeddbtransport.Server
	relay      *http.Server
	listener   net.Listener
	socketPath string
}

func startTLSRelay(address string, certFile string, keyFile string, expectToken string) (*relayHarness, error) {
	socketPath := filepath.Join(
		os.TempDir(),
		fmt.Sprintf("idb-relay-%d-%d.sock", os.Getpid(), time.Now().UnixNano()),
	)
	backend, err := indexeddbtransport.Start("unix://"+socketPath, indexeddbtransport.Options{})
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		backend.Stop()
		return nil, err
	}

	proxy := newRuntimeRelayProxy(socketPath)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if expectToken != "" {
				token := strings.TrimSpace(r.Header.Get(runtimehost.HostServiceRelayTokenHeader))
				if token != expectToken {
					writeGRPCTrailersOnly(w, codes.Unauthenticated, "invalid-host-service-relay-token")
					return
				}
			}
			proxy.ServeHTTP(w, r)
		}),
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2"},
		},
	}
	go func() {
		_ = server.ServeTLS(listener, certFile, keyFile)
	}()

	return &relayHarness{
		backend:    backend,
		relay:      server,
		listener:   listener,
		socketPath: socketPath,
	}, nil
}

func (h *relayHarness) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = h.relay.Shutdown(ctx)
	_ = h.listener.Close()
	h.backend.Stop()
	_ = os.Remove(h.socketPath)
}

func newRuntimeRelayProxy(socketPath string) *httputil.ReverseProxy {
	target := "gestalt-test-host-service-relay"
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = target
			pr.Out.Host = target
			pr.Out.Header.Del(runtimehost.HostServiceRelayTokenHeader)
		},
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, _, _ string, _ *tls.Config) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeGRPCTrailersOnly(w, codes.Unavailable, "host-service-relay-unavailable")
		},
	}
}

func writeGRPCTrailersOnly(w http.ResponseWriter, code codes.Code, message string) {
	headers := w.Header()
	headers.Set("Content-Type", "application/grpc")
	headers.Set("Trailer", "Grpc-Status, Grpc-Message")
	headers.Set("Grpc-Status", strconv.Itoa(int(code)))
	if message != "" {
		headers.Set("Grpc-Message", message)
	}
	w.WriteHeader(http.StatusOK)
}
