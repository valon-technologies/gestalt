// Command cachetransportd runs a minimal Cache gRPC server on a Unix socket for
// SDK transport tests. It prints READY to stdout when listening and serves
// until killed.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/valon-technologies/gestalt/server/services/testutil/cachetransport"
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
		fmt.Fprintln(os.Stderr, "usage: cachetransportd --socket <path> | --tcp <host:port>")
		os.Exit(1)
	}

	srv, err := cachetransport.Start(target, cachetransport.Options{
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
