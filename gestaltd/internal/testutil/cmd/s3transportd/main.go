// Command s3transportd runs a minimal S3 gRPC server on a Unix socket for SDK
// transport tests. It prints READY to stdout when listening and serves until
// killed.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/valon-technologies/gestalt/server/internal/testutil/s3transport"
)

func main() {
	socket := flag.String("socket", "", "Unix socket path to listen on")
	flag.Parse()
	if *socket == "" {
		fmt.Fprintln(os.Stderr, "usage: s3transportd --socket <path>")
		os.Exit(1)
	}

	srv, err := s3transport.Start(*socket)
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
