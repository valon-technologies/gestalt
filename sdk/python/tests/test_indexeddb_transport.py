"""Transport-backed IndexedDB SDK tests over a real Unix socket."""

from __future__ import annotations

import os
import shutil
import socket
import subprocess
import tempfile
import unittest

import grpc

from gestalt import (
    CURSOR_NEXT,
    AlreadyExistsError,
    IndexedDB,
    IndexSchema,
    KeyRange,
    NotFoundError,
    ObjectStoreSchema,
    TransactionError,
    indexeddb_socket_env,
    indexeddb_socket_token_env,
)


def _build_harness() -> str:
    """Build the Go harness binary and return its path."""
    bin_path = os.path.join(tempfile.gettempdir(), "indexeddbtransportd")
    src_dir = os.path.join(
        os.path.dirname(__file__),
        "..",
        "..",
        "..",
        "gestaltd",
        "internal",
        "testutil",
        "cmd",
        "indexeddbtransportd",
    )
    subprocess.check_call(
        ["go", "build", "-o", bin_path, "."],
        cwd=os.path.abspath(src_dir),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return bin_path


_harness_bin: str | None = None
_harness_proc: subprocess.Popen[bytes] | None = None
_socket_path: str = ""

_TLS_CERT_PEM = """-----BEGIN CERTIFICATE-----
MIIDJTCCAg2gAwIBAgIUaQujB8wIeckTiFgjFgZbVaxm3EQwDQYJKoZIhvcNAQEL
BQAwFDESMBAGA1UEAwwJMTI3LjAuMC4xMB4XDTI2MDQyNDE0NDA0MFoXDTM2MDQy
MTE0NDA0MFowFDESMBAGA1UEAwwJMTI3LjAuMC4xMIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEAsDkKPwQrtsaP9/AgIhbEy37Qo04MNGFu58T0HihDs3YF
yGtJmiUv99dYYzsErAuPBIlITaDJqi3jtIpg8mgQZsokzkqeWtYG14Xy2fejwzIc
Me4i4nE9Pj5WICJ2ydmERS2cOiDhUMojtaMgGv+kIZFOfF6/TpL6eyRPa2pRfgI0
cbKltvafs0PGL66IchCGAOd1i+iDRCJcUC7Gle4vYwZyg0a3ojGt8+ufySmNxN1m
gNHcAXU81xoqwfiuk62sDaz3Ev7kEsXp3nQf6Pv+3LTQjR8tcAh7DuD5QwEW+o66
N3SmR+hX8KWKXGqa1j4G6hKkkCB5x1sUgRHagyVaRwIDAQABo28wbTAdBgNVHQ4E
FgQU9PEc5NZcAXX9Hp46kuJT/8OpN0IwHwYDVR0jBBgwFoAU9PEc5NZcAXX9Hp46
kuJT/8OpN0IwDwYDVR0TAQH/BAUwAwEB/zAaBgNVHREEEzARhwR/AAABgglsb2Nh
bGhvc3QwDQYJKoZIhvcNAQELBQADggEBAJtCNvqECQmT7PrbErt9VzmwIok5JyFd
RIkWdSTAYREET5PFK11BdKLcatrjNEedZ94X3M27dOpTDXhGhu2W2n/bg+6QKiEN
UeZcDja28w50vrkJTSFTYgON96nX6zPd3mtVY8Z2gAE8DWtMnjfzDj26354dldht
yHJtQIuj/67p6FDgu+nXVz84UtmH1PAoJ6/dSMTjpjMdDSLo0P44EHK80curNKdF
u67O4clOBfuyR96gqeMXpx2qy6T4y1kOTcIecVjG/U/BD31xgRxqBhf7JXV40QDA
zqha1yhGmVTSc6eS8FX/oD/QGmqbiQJmdprbop05KLItfi1FAE0yTvs=
-----END CERTIFICATE-----
"""

_TLS_KEY_PEM = """-----BEGIN PRIVATE KEY-----
MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQCwOQo/BCu2xo/3
8CAiFsTLftCjTgw0YW7nxPQeKEOzdgXIa0maJS/311hjOwSsC48EiUhNoMmqLeO0
imDyaBBmyiTOSp5a1gbXhfLZ96PDMhwx7iLicT0+PlYgInbJ2YRFLZw6IOFQyiO1
oyAa/6QhkU58Xr9Okvp7JE9ralF+AjRxsqW29p+zQ8YvrohyEIYA53WL6INEIlxQ
LsaV7i9jBnKDRreiMa3z65/JKY3E3WaA0dwBdTzXGirB+K6TrawNrPcS/uQSxene
dB/o+/7ctNCNHy1wCHsO4PlDARb6jro3dKZH6FfwpYpcaprWPgbqEqSQIHnHWxSB
EdqDJVpHAgMBAAECggEACpDo+d1Mp7FhKXsW2iRmWVM5vEjuN2fOKAxpnLNKV+TQ
NPOl3p2zMheR36VGwvAQe7Olh64H2XHV8NnJNU+jCB6/tTTJKOYjU+HerU4JXidP
hHjkU5J5mxVOwa9/Utv9b85ryxp0mAz+tiHZR3UjiLW3MILX0qTCawbC0kx2JWlv
8kDri5JK9Cbe1TpfiDPg+up//Mu9KltPuB8yaBAGgj3JmafdNfQWDNh/UnK0O0VG
TzFyeQN998RqJc0zXvPds+x5k8BMG955HuX2EA/SjyD01IrT2BtLyrRaw3ovlSCS
FhWzMf/85mUgOkKzXm3aTqjVnL+C/JHTlx21+ypw3QKBgQDb0V060PbUI1T6vL0d
jqkN0AO6o/OWikglVUJ0v9nm4N7ueU6ODtx2NNjsyvHPvLodIGTAkplU55+VBhmf
dz2JG3XLvTOxSyr75k/T4d28FkscrLphefutWx0UY2aTp6YXlMkQ+jzARiEESKmU
KZgR03p+JxAkZmWb7cGJz45EBQKBgQDNOq203/Nyq8uM7s8ZnGX943ZR5oI716Bs
X519C5IDwngMoDxySvdncHf2EROSxmciIH/C5i4oJ5yZCtdDOD2rSFR0DvQF2WKk
ZIsJzZmLxt4wu7R6/HU+JeU4LSp0QRSpXj72fdXhUuRARznNbufnkbccPe12ww09
XAQNiq6i2wKBgDMljuzNjHEl23MQEWzcMee92/BEj7waZtkQ8oqZzUjUT+rrHOUe
/hsfBs5qFkPA5Qk77VWFhtnjnxUcuz+Iji/lzM3gMzPwiorcNvzVFDPceBOu+RsP
OAlJJwYEbuyyWIoqG3Kw1wviBXKquZJ47yJOs7TAwBfIH6Jdeufm/HJFAoGAEQEJ
n3DmxNuDE/w9YIvaz3xnM0X8CGVHP3N0owWwZWtZcwJbv8SCVym0ZsjnbEPQC73R
mB5mOKF/khaZ21Hvmh92D9+lTE7Eo4ZJFtjYHgKuKi+DNqVwOWP+Z/cmC1fRFG9g
nB+09uRdUQ4VtfW4dTFXkJl48Vwb3refBlg1O/0CgYBwKoyGmlcQfQAX0b29835W
gWukLcKU+Kd99IAuVVbWNsAFwqEp0cm3gtrN6ZcPQ/U095WqsrGe7Bt2qw7xSt5D
Jpp6kBddjP+CvphxEnoP0AQY71BBvW25NYxecd16Mhk5/g2/h/etwpnyE/XCXbOS
2Xz7l2ukjglD2aCfiHN+EQ==
-----END PRIVATE KEY-----
"""


def setUpModule() -> None:
    global _harness_bin, _harness_proc, _socket_path
    _harness_bin = _build_harness()
    _socket_path = os.path.join(
        tempfile.gettempdir(), f"py-idb-test-{os.getpid()}.sock"
    )
    _harness_proc = subprocess.Popen(
        [_harness_bin, "--socket", _socket_path],
        stdout=subprocess.PIPE,
    )
    assert _harness_proc.stdout is not None
    line = _harness_proc.stdout.readline().decode().strip()
    _harness_proc.stdout.close()
    if line != "READY":
        _harness_proc.kill()
        raise RuntimeError(f"harness did not print READY, got: {line!r}")
    os.environ["GESTALT_INDEXEDDB_SOCKET"] = _socket_path
    os.environ[indexeddb_socket_env("named")] = f"unix://{_socket_path}"


def tearDownModule() -> None:
    if _harness_proc:
        _harness_proc.kill()
        _harness_proc.wait()
    if _socket_path and os.path.exists(_socket_path):
        os.remove(_socket_path)


def _reserve_tcp_address() -> str:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        host, port = sock.getsockname()
    return f"{host}:{port}"


def _start_tcp_harness(expect_token: str = "") -> tuple[subprocess.Popen[bytes], str]:
    harness_bin = _build_harness()
    address = _reserve_tcp_address()
    args = [harness_bin, "--tcp", address]
    if expect_token:
        args.extend(["--expect-token", expect_token])
    proc = subprocess.Popen(
        args,
        stdout=subprocess.PIPE,
    )
    assert proc.stdout is not None
    line = proc.stdout.readline().decode().strip()
    proc.stdout.close()
    if line != "READY":
        proc.kill()
        raise RuntimeError(f"tcp harness did not print READY, got: {line!r}")
    return proc, f"tcp://{address}"


def _start_tls_relay_harness(
    expect_token: str = "",
) -> tuple[subprocess.Popen[bytes], str, str, str, str]:
    harness_bin = _build_harness()
    address = _reserve_tcp_address()
    temp_dir = tempfile.mkdtemp(prefix="py-idb-tls-relay-")
    cert_path = os.path.join(temp_dir, "cert.pem")
    key_path = os.path.join(temp_dir, "key.pem")
    with open(cert_path, "w", encoding="utf-8") as cert_file:
        cert_file.write(_TLS_CERT_PEM)
    with open(key_path, "w", encoding="utf-8") as key_file:
        key_file.write(_TLS_KEY_PEM)
    args = [
        harness_bin,
        "--relay-tls",
        address,
        "--cert-file",
        cert_path,
        "--key-file",
        key_path,
    ]
    if expect_token:
        args.extend(["--expect-token", expect_token])
    proc = subprocess.Popen(
        args,
        stdout=subprocess.PIPE,
    )
    assert proc.stdout is not None
    line = proc.stdout.readline().decode().strip()
    proc.stdout.close()
    if line != "READY":
        proc.kill()
        raise RuntimeError(f"tls relay harness did not print READY, got: {line!r}")
    return proc, f"tls://{address}", cert_path, key_path, temp_dir


def _client() -> IndexedDB:
    return IndexedDB()


def _set_env(testcase: unittest.TestCase, name: str, value: str) -> None:
    previous = os.environ.get(name)
    os.environ[name] = value

    def restore() -> None:
        if previous is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = previous

    testcase.addCleanup(restore)


def _seed_store(client: IndexedDB, name: str) -> None:
    schema = ObjectStoreSchema(
        indexes=[
            IndexSchema(name="by_status", key_path=["status"], unique=False),
            IndexSchema(name="by_email", key_path=["email"], unique=True),
        ],
    )
    client.create_object_store(name, schema)
    store = client.object_store(name)
    store.add(
        {"id": "a", "name": "Alice", "status": "active", "email": "alice@test.com"}
    )
    store.add({"id": "b", "name": "Bob", "status": "active", "email": "bob@test.com"})
    store.add(
        {"id": "c", "name": "Carol", "status": "inactive", "email": "carol@test.com"}
    )
    store.add({"id": "d", "name": "Dave", "status": "active", "email": "dave@test.com"})


class TestNestedJSON(unittest.TestCase):
    def test_nested_json_roundtrip(self) -> None:
        c = _client()
        c.create_object_store("nested_json")
        s = c.object_store("nested_json")
        s.put(
            {"id": "r1", "meta": {"role": "admin", "level": 5}, "tags": ["rust", "go"]}
        )
        got = s.get("r1")
        self.assertIsInstance(got["meta"], dict)
        self.assertEqual(got["meta"]["role"], "admin")
        self.assertIsInstance(got["tags"], list)
        self.assertEqual(got["tags"][0], "rust")
        c.close()


class TestTransaction(unittest.TestCase):
    def test_readwrite_commits_and_reads_own_writes(self) -> None:
        c = _client()
        c.create_object_store("transaction_commit")
        s = c.object_store("transaction_commit")

        with c.transaction(["transaction_commit"], "readwrite") as tx:
            txs = tx.object_store("transaction_commit")
            txs.put({"id": "lease-1", "owner": "worker-a", "version": 1})
            self.assertEqual(txs.get("lease-1")["owner"], "worker-a")
            txs.put({"id": "lease-1", "owner": "worker-b", "version": 2})
            self.assertEqual(txs.count(), 1)

        self.assertEqual(s.get("lease-1")["owner"], "worker-b")
        c.close()

    def test_abort_rolls_back(self) -> None:
        c = _client()
        c.create_object_store("transaction_abort")
        s = c.object_store("transaction_abort")

        tx = c.transaction(["transaction_abort"], "readwrite")
        tx.object_store("transaction_abort").put({"id": "row-1", "value": "pending"})
        tx.abort()

        with self.assertRaises(NotFoundError):
            s.get("row-1")
        c.close()

    def test_readonly_rejects_writes(self) -> None:
        c = _client()
        c.create_object_store("transaction_readonly")
        s = c.object_store("transaction_readonly")
        s.put({"id": "row-1", "value": "kept"})

        tx = c.transaction(["transaction_readonly"], "readonly")
        self.assertEqual(
            tx.object_store("transaction_readonly").get("row-1")["value"], "kept"
        )
        with self.assertRaises(TransactionError):
            tx.object_store("transaction_readonly").put(
                {"id": "row-2", "value": "blocked"}
            )

        with self.assertRaises(NotFoundError):
            s.get("row-2")
        c.close()

    def test_operation_error_rolls_back(self) -> None:
        c = _client()
        c.create_object_store("transaction_error_rollback")
        s = c.object_store("transaction_error_rollback")

        with self.assertRaises(AlreadyExistsError):
            with c.transaction(["transaction_error_rollback"], "readwrite") as tx:
                txs = tx.object_store("transaction_error_rollback")
                txs.add({"id": "row-1", "value": "pending"})
                txs.add({"id": "row-1", "value": "duplicate"})

        with self.assertRaises(NotFoundError):
            s.get("row-1")
        c.close()

    def test_index_operations_and_bulk_deletes_roll_back(self) -> None:
        c = _client()
        c.create_object_store(
            "transaction_index_bulk_rollback",
            ObjectStoreSchema(
                indexes=[IndexSchema(name="by_status", key_path=["status"])]
            ),
        )
        s = c.object_store("transaction_index_bulk_rollback")
        for id, status in [
            ("a", "active"),
            ("b", "active"),
            ("c", "inactive"),
            ("d", "active"),
        ]:
            s.add({"id": id, "status": status})

        tx = c.transaction(["transaction_index_bulk_rollback"], "readwrite")
        txs = tx.object_store("transaction_index_bulk_rollback")
        self.assertEqual(txs.index("by_status").count("active"), 3)
        self.assertEqual(len(txs.index("by_status").get_all_keys("active")), 3)
        self.assertEqual(txs.delete_range(KeyRange(lower="b", upper="c")), 2)
        self.assertEqual(txs.index("by_status").delete("active"), 2)
        txs.clear()
        self.assertEqual(txs.count(), 0)
        tx.abort()

        self.assertEqual(s.count(), 4)
        self.assertEqual(s.index("by_status").count("inactive"), 1)
        c.close()


class TestNamedSocketEnv(unittest.TestCase):
    def test_named_socket_env_roundtrip(self) -> None:
        c = IndexedDB("named")
        c.create_object_store("named_socket_env")
        s = c.object_store("named_socket_env")
        s.put({"id": "row-1", "value": "named"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "named")
        c.close()


class TestTCPTarget(unittest.TestCase):
    def test_tcp_target_roundtrip(self) -> None:
        proc, target = _start_tcp_harness()
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)
        _set_env(self, "GESTALT_INDEXEDDB_SOCKET", target)

        c = _client()
        c.create_object_store("tcp_target_env")
        s = c.object_store("tcp_target_env")
        s.put({"id": "row-1", "value": "tcp"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "tcp")
        c.close()

    def test_tcp_target_token_roundtrip(self) -> None:
        token = "relay-token-python"
        proc, target = _start_tcp_harness(expect_token=token)
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)
        _set_env(self, "GESTALT_INDEXEDDB_SOCKET", target)
        _set_env(self, indexeddb_socket_token_env(), token)

        c = _client()
        c.create_object_store("tcp_target_token_env")
        s = c.object_store("tcp_target_token_env")
        s.put({"id": "row-1", "value": "token"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "token")
        c.close()

    def test_tcp_target_ignores_proxy_env(self) -> None:
        token = "relay-token-python"
        proc, target = _start_tcp_harness(expect_token=token)
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)

        target_env = "GESTALT_INDEXEDDB_SOCKET"
        token_env = indexeddb_socket_token_env()
        previous_target = os.environ.get(target_env)
        previous_token = os.environ.get(token_env)
        previous_http_proxy = os.environ.get("http_proxy")
        previous_https_proxy = os.environ.get("https_proxy")

        def restore_env() -> None:
            if previous_target is None:
                os.environ.pop(target_env, None)
            else:
                os.environ[target_env] = previous_target
            if previous_token is None:
                os.environ.pop(token_env, None)
            else:
                os.environ[token_env] = previous_token
            if previous_http_proxy is None:
                os.environ.pop("http_proxy", None)
            else:
                os.environ["http_proxy"] = previous_http_proxy
            if previous_https_proxy is None:
                os.environ.pop("https_proxy", None)
            else:
                os.environ["https_proxy"] = previous_https_proxy

        os.environ[target_env] = target
        os.environ[token_env] = token
        os.environ["http_proxy"] = "http://127.0.0.1:1"
        os.environ["https_proxy"] = "http://127.0.0.1:1"
        self.addCleanup(restore_env)

        c = _client()
        c.create_object_store("tcp_target_proxy_env")
        s = c.object_store("tcp_target_proxy_env")
        s.put({"id": "row-1", "value": "proxy-bypass"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "proxy-bypass")
        c.close()


class TestTLSRelayTarget(unittest.TestCase):
    def test_tls_relay_target_token_roundtrip_ignores_proxy_env(self) -> None:
        token = "relay-token-python"
        proc, target, cert_path, _, temp_dir = _start_tls_relay_harness(
            expect_token=token
        )
        self.addCleanup(proc.wait)
        self.addCleanup(proc.kill)
        self.addCleanup(shutil.rmtree, temp_dir, True)

        restore_names = [
            "GESTALT_INDEXEDDB_SOCKET",
            indexeddb_socket_token_env(),
            "GRPC_DEFAULT_SSL_ROOTS_FILE_PATH",
            "http_proxy",
            "https_proxy",
            "HTTP_PROXY",
            "HTTPS_PROXY",
        ]
        previous_env = {name: os.environ.get(name) for name in restore_names}

        def restore_env() -> None:
            for name, value in previous_env.items():
                if value is None:
                    os.environ.pop(name, None)
                else:
                    os.environ[name] = value

        os.environ["GESTALT_INDEXEDDB_SOCKET"] = target
        os.environ[indexeddb_socket_token_env()] = token
        os.environ["GRPC_DEFAULT_SSL_ROOTS_FILE_PATH"] = cert_path
        os.environ["http_proxy"] = "http://127.0.0.1:1"
        os.environ["https_proxy"] = "http://127.0.0.1:1"
        os.environ["HTTP_PROXY"] = "http://127.0.0.1:1"
        os.environ["HTTPS_PROXY"] = "http://127.0.0.1:1"
        self.addCleanup(restore_env)

        c = _client()
        c.create_object_store("tls_relay_target_env")
        s = c.object_store("tls_relay_target_env")
        s.put({"id": "row-1", "value": "tls-relay"})
        got = s.get("row-1")
        self.assertEqual(got["value"], "tls-relay")
        c.close()


class TestCursorHappyPath(unittest.TestCase):
    def test_cursor_iterates_all_records(self) -> None:
        c = _client()
        _seed_store(c, "cursor_happy")
        s = c.object_store("cursor_happy")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            keys: list[str] = []
            while cursor.continue_():
                keys.append(cursor.primary_key or "")
        self.assertEqual(len(keys), 4)
        self.assertEqual(keys, sorted(keys))
        c.close()


class TestEmptyCursor(unittest.TestCase):
    def test_empty_store_cursor(self) -> None:
        c = _client()
        c.create_object_store("empty_cursor")
        s = c.object_store("empty_cursor")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertFalse(cursor.continue_())
        c.close()


class TestKeysOnlyCursor(unittest.TestCase):
    def test_keys_only_value_raises(self) -> None:
        c = _client()
        _seed_store(c, "keys_only")
        s = c.object_store("keys_only")
        with s.open_key_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertTrue(cursor.continue_())
            self.assertIsNotNone(cursor.primary_key)
            with self.assertRaises(TypeError):
                _ = cursor.value
        c.close()


class TestCursorExhaustion(unittest.TestCase):
    def test_no_error_after_exhaustion(self) -> None:
        c = _client()
        _seed_store(c, "exhaustion")
        s = c.object_store("exhaustion")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            count = 0
            while cursor.continue_():
                count += 1
            self.assertEqual(count, 4)
        c.close()


class TestContinueToKeyBeyondEnd(unittest.TestCase):
    def test_returns_false(self) -> None:
        c = _client()
        _seed_store(c, "ctk_beyond")
        s = c.object_store("ctk_beyond")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertFalse(cursor.continue_to_key("zzz"))
        c.close()


class TestAdvancePastEnd(unittest.TestCase):
    def test_returns_false(self) -> None:
        c = _client()
        _seed_store(c, "advance_past")
        s = c.object_store("advance_past")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertFalse(cursor.advance(100))
        c.close()


class TestCursorCloseClearsState(unittest.TestCase):
    def test_close_clears_last_entry(self) -> None:
        c = _client()
        _seed_store(c, "close_clears_state")
        s = c.object_store("close_clears_state")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertTrue(cursor.continue_())
            self.assertEqual(cursor.primary_key, "a")

        self.assertIsNone(cursor.key)
        self.assertIsNone(cursor.primary_key)
        with self.assertRaises(TypeError):
            _ = cursor.value
        c.close()


class TestAdvanceRejectsNonPositiveCounts(unittest.TestCase):
    def test_zero_raises_invalid_argument(self) -> None:
        c = _client()
        _seed_store(c, "advance_invalid")
        s = c.object_store("advance_invalid")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            with self.assertRaises(grpc.RpcError):
                cursor.advance(0)
        c.close()


class TestPostExhaustion(unittest.TestCase):
    def test_post_exhaustion_behavior(self) -> None:
        c = _client()
        _seed_store(c, "post_exhaust")
        s = c.object_store("post_exhaust")
        with s.open_cursor(direction=CURSOR_NEXT) as cursor:
            while cursor.continue_():
                pass
            self.assertFalse(cursor.continue_())
            with self.assertRaises(NotFoundError):
                cursor.delete()
        c.close()


class TestIndexCursor(unittest.TestCase):
    def test_index_filter(self) -> None:
        c = _client()
        _seed_store(c, "index_cursor")
        s = c.object_store("index_cursor")
        with s.index("by_status").open_cursor(
            "active", direction=CURSOR_NEXT
        ) as cursor:
            count = 0
            while cursor.continue_():
                self.assertEqual(cursor.value["status"], "active")
                count += 1
            self.assertEqual(count, 3)
        c.close()


class TestIndexContinueToKey(unittest.TestCase):
    def test_round_trips_cursor_key(self) -> None:
        c = _client()
        c.create_object_store(
            "index_seek",
            ObjectStoreSchema(
                indexes=[IndexSchema(name="by_num", key_path=["n"], unique=False)]
            ),
        )
        s = c.object_store("index_seek")
        s.add({"id": "a", "n": 1})
        s.add({"id": "b", "n": 2})
        s.add({"id": "c", "n": 3})

        with s.index("by_num").open_cursor(direction=CURSOR_NEXT) as cursor:
            self.assertTrue(cursor.continue_())
            self.assertEqual(cursor.key, [1])
            self.assertTrue(cursor.continue_to_key(cursor.key))
            self.assertEqual(cursor.primary_key, "b")
        c.close()


class TestErrorMapping(unittest.TestCase):
    def test_not_found(self) -> None:
        c = _client()
        c.create_object_store("err_map")
        s = c.object_store("err_map")
        with self.assertRaises(NotFoundError):
            s.get("nonexistent")
        c.close()

    def test_already_exists(self) -> None:
        c = _client()
        c.create_object_store("err_map_dup")
        s = c.object_store("err_map_dup")
        s.add({"id": "x"})
        with self.assertRaises(AlreadyExistsError):
            s.add({"id": "x"})
        c.close()
