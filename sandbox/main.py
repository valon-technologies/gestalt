"""Gestalt sandbox process entry point.

Starts a gRPC server implementing SandboxService and connects to the
Go host's ToolService for executing integration operations.
"""
import argparse
import logging
import signal
from concurrent import futures

import grpc

from sandbox.pb import sandbox_pb2_grpc
from sandbox.service import SandboxServicer
from sandbox.tools import ToolClient

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)
log = logging.getLogger("sandbox")


def main():
    parser = argparse.ArgumentParser(description="Gestalt sandbox process")
    parser.add_argument("--listen-addr", required=True,
                        help="Address for the gRPC server (e.g. unix:///tmp/sandbox.sock)")
    parser.add_argument("--tool-service-addr", required=True,
                        help="Address of the Go ToolService (e.g. localhost:50051)")
    args = parser.parse_args()

    tool_client = ToolClient(args.tool_service_addr)
    servicer = SandboxServicer(tool_client)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=2))
    sandbox_pb2_grpc.add_SandboxServiceServicer_to_server(servicer, server)
    server.add_insecure_port(args.listen_addr)
    server.start()
    log.info("sandbox listening on %s", args.listen_addr)

    def handle_signal(signum, frame):
        log.info("received signal %s, shutting down", signum)
        server.stop(grace=5)
        tool_client.close()

    signal.signal(signal.SIGTERM, handle_signal)
    signal.signal(signal.SIGINT, handle_signal)

    server.wait_for_termination()


if __name__ == "__main__":
    main()
