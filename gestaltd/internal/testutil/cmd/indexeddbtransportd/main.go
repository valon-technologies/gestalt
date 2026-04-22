// Command indexeddbtransportd runs a minimal IndexedDB gRPC server for SDK
// transport tests. It prints READY to stdout when listening and serves until
// killed.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/valon-technologies/gestalt/server/internal/testutil/indexeddbtransport"
)

func main() {
	socket := flag.String("socket", "", "Unix socket path to listen on")
	tcp := flag.String("tcp", "", "TCP host:port to listen on")
	expectToken := flag.String("expect-token", "", "Require the relay token header to match this value")
	flag.Parse()
	target := *socket
	if strings.TrimSpace(*tcp) != "" {
		target = "tcp://" + strings.TrimSpace(*tcp)
	}
	if strings.TrimSpace(target) == "" {
		fmt.Fprintln(os.Stderr, "usage: indexeddbtransportd --socket <path> | --tcp <host:port>")
		os.Exit(1)
	}

	srv, err := indexeddbtransport.Start(target, indexeddbtransport.Options{
		ExpectRelayToken: strings.TrimSpace(*expectToken),
	})
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
