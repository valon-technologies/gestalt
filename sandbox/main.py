import argparse
import logging
import signal
import threading
from concurrent import futures

import grpc

from sandbox.pb import sandbox_pb2_grpc
from sandbox.service import SandboxServicer
from sandbox.tools import ToolClient

log = logging.getLogger(__name__)


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")

    parser = argparse.ArgumentParser(description="Gestalt sandbox runner")
    parser.add_argument("--listen-addr", required=True, help="gRPC listen address (e.g. localhost:50051)")
    parser.add_argument("--tool-service-addr", required=True, help="Go ToolService gRPC address")
    args = parser.parse_args()

    tool_client = ToolClient(args.tool_service_addr)
    servicer = SandboxServicer(tool_client)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    sandbox_pb2_grpc.add_SandboxServiceServicer_to_server(servicer, server)
    server.add_insecure_port(args.listen_addr)

    stop_event = threading.Event()

    def handle_signal(signum, frame):
        log.info("received signal %s, shutting down", signum)
        stop_event.set()

    signal.signal(signal.SIGTERM, handle_signal)
    signal.signal(signal.SIGINT, handle_signal)

    server.start()
    log.info("sandbox server listening on %s", args.listen_addr)

    stop_event.wait()

    server.stop(grace=5)
    tool_client.close()
    log.info("sandbox server stopped")


if __name__ == "__main__":
    main()
