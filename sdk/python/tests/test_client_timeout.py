"""Client-level timeout tests against a sleeping in-process servicer."""

from __future__ import annotations

import time
import unittest
from concurrent import futures
from typing import Any

import grpc

from gestalt import Cache
from gestalt._gen.v1 import cache_pb2 as _cache_pb2
from gestalt._gen.v1 import cache_pb2_grpc as _cache_pb2_grpc
from gestalt.rpc_support import GestaltError, GestaltErrorCode

cache_pb2: Any = _cache_pb2
cache_pb2_grpc: Any = _cache_pb2_grpc


class _SleepingCacheServicer(cache_pb2_grpc.CacheServicer):
    def Get(self, request, context):
        time.sleep(1.0)
        return cache_pb2.CacheGetResponse(found=True, value=b"slow")


class ClientTimeoutTests(unittest.TestCase):
    def test_unary_deadline_exceeded(self) -> None:
        server = grpc.server(futures.ThreadPoolExecutor(max_workers=1))
        cache_pb2_grpc.add_CacheServicer_to_server(_SleepingCacheServicer(), server)
        port = server.add_insecure_port("127.0.0.1:0")
        server.start()
        self.addCleanup(server.stop, 0)

        with grpc.insecure_channel(f"127.0.0.1:{port}") as channel:
            client = Cache(channel, timeout=0.05)
            with self.assertRaises(GestaltError) as caught:
                client.get(key="slow")
        self.assertEqual(caught.exception.code, GestaltErrorCode.DEADLINE_EXCEEDED)


if __name__ == "__main__":
    unittest.main()
