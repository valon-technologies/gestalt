package runtimehost

import (
	"time"

	"google.golang.org/grpc"
)

const hostServiceShutdownTimeout = 2 * time.Second

func stopGRPCServer(srv *grpc.Server, timeout time.Duration) {
	if srv == nil {
		return
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		srv.GracefulStop()
	}()

	if timeout <= 0 {
		<-stopped
		return
	}

	select {
	case <-stopped:
	case <-time.After(timeout):
		srv.Stop()
		<-stopped
	}
}
