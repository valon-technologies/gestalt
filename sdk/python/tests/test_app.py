import json
import pathlib
import tempfile
import unittest
from dataclasses import dataclass

from gestalt import OK, Access, App, Credential, Error, Request, Response, Subject


class AppOperationTests(unittest.TestCase):
    """Tests for App operation registration and execution using real handlers."""

    def test_register_and_execute_operation(self) -> None:
        """Registering an operation and executing it should return the handler's result."""
        app = App("test-plugin")

        @app.operation
        def greet() -> dict[str, str]:
            return {"message": "hello"}

        result = app.execute("greet", {}, Request())
        self.assertEqual(result.status, 200)
        self.assertEqual(json.loads(result.body), {"message": "hello"})

    def test_execute_missing_operation(self) -> None:
        """Executing a non-existent operation should return 404."""
        app = App("test-plugin")

        result = app.execute("missing", {}, Request())
        self.assertEqual(result.status, 404)

    def test_operation_with_input(self) -> None:
        """Operations with typed input should decode params correctly."""
        app = App("test-plugin")

        @dataclass
        class Input:
            name: str
            count: int = 1

        @app.operation
        def greet(input: Input) -> dict[str, str]:
            return {"message": f"hello {input.name} x{input.count}"}

        result = app.execute("greet", {"name": "world", "count": 3}, Request())
        self.assertEqual(result.status, 200)
        body = json.loads(result.body)
        self.assertEqual(body["message"], "hello world x3")

    def test_operation_with_response_wrapper(self) -> None:
        """Operations returning Response should preserve status and body."""
        app = App("test-plugin")

        @app.operation
        def created() -> Response[dict[str, str]]:
            return Response(
                status=201,
                body={"id": "abc"},
                headers={"Location": "/items/abc"},
            )

        result = app.execute("created", {}, Request())
        self.assertEqual(result.status, 201)
        self.assertEqual(result.headers["Content-Type"], ["application/json"])
        self.assertEqual(result.headers["Location"], ["/items/abc"])
        self.assertEqual(json.loads(result.body), {"id": "abc"})

    def test_ok_helper(self) -> None:
        """The OK() helper should produce status 200."""
        app = App("test-plugin")

        @app.operation
        def ok_op() -> Response[str]:
            return OK("done")

        result = app.execute("ok_op", {}, Request())
        self.assertEqual(result.status, 200)

    def test_operation_with_custom_id(self) -> None:
        """Operations can specify a custom ID separate from the function name."""
        app = App("test-plugin")

        @app.operation(id="custom-id", method="GET")
        def handler() -> str:
            return "ok"

        result = app.execute("custom-id", {}, Request())
        self.assertEqual(result.status, 200)

    def test_duplicate_operation_id_raises(self) -> None:
        """Registering two operations with the same ID should raise."""
        app = App("test-plugin")

        @app.operation(id="dup")
        def first() -> str:
            return "first"

        with self.assertRaises(ValueError, msg="duplicate operation id"):

            @app.operation(id="dup")
            def second() -> str:
                return "second"

    def test_handler_receives_request(self) -> None:
        """Operations that take Request should receive host request metadata."""
        app = App("test-plugin")

        @app.operation
        def echo(req: Request) -> dict[str, str]:
            return {
                "token": req.token,
                "region": req.connection_param("region") or "",
                "subject_id": req.subject.id,
                "credential_mode": req.credential.mode,
                "access_role": req.access.role,
            }

        result = app.execute(
            "echo",
            {},
            Request(
                token="tok-abc",
                connection_params={"region": "us-east-1"},
                subject=Subject(id="user:user-123"),
                credential=Credential(mode="subject"),
                access=Access(role="admin"),
            ),
        )
        body = json.loads(result.body)
        self.assertEqual(body["token"], "tok-abc")
        self.assertEqual(body["region"], "us-east-1")
        self.assertEqual(body["subject_id"], "user:user-123")
        self.assertEqual(body["credential_mode"], "subject")
        self.assertEqual(body["access_role"], "admin")

    def test_async_handler(self) -> None:
        app = App("test-plugin")

        @app.operation
        async def fetch() -> dict[str, str]:
            return {"async": "result"}

        result = app.execute("fetch", {}, Request())
        self.assertEqual(result.status, 200)
        self.assertEqual(json.loads(result.body), {"async": "result"})

    def test_handler_exception_returns_500(self) -> None:
        """Handler exceptions should preserve explicit statuses and default to 500 otherwise."""
        app = App("test-plugin")

        @app.operation
        def broken() -> None:
            raise RuntimeError("something broke")

        @app.operation
        def missing() -> None:
            raise Error(404, "record not found")

        result = app.execute("broken", {}, Request())
        self.assertEqual(result.status, 500)
        self.assertEqual(json.loads(result.body), {"error": "internal error"})

        result = app.execute("missing", {}, Request())
        self.assertEqual(result.status, 404)
        self.assertEqual(json.loads(result.body), {"error": "record not found"})


class AppConfigureTests(unittest.TestCase):
    """Tests for the @app.configure decorator."""

    def test_configure_handler_called(self) -> None:
        """The configure handler should be called with name and config."""
        app = App("test-plugin")
        calls: list[tuple[str, dict[str, str]]] = []

        @app.configure
        def setup(name: str, config: dict[str, str]) -> None:
            calls.append((name, config))

        app.configure_provider("my-provider", {"key": "value"})

        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0], ("my-provider", {"key": "value"}))

    def test_no_configure_handler_is_noop(self) -> None:
        """Without a configure handler, configure_provider should be a no-op."""
        app = App("test-plugin")
        app.configure_provider("my-provider", {"key": "value"})


class AppCatalogTests(unittest.TestCase):
    """Tests for catalog generation."""

    def test_catalog_dict(self) -> None:
        """catalog_dict should return the app name and operation list."""
        app = App("test-plugin")

        @app.operation(method="GET", description="Say hello", read_only=True)
        def greet() -> str:
            return "hello"

        catalog = app.catalog_dict()
        self.assertEqual(catalog["name"], "test-plugin")
        self.assertEqual(len(catalog["operations"]), 1)
        op = catalog["operations"][0]
        self.assertEqual(op["id"], "greet")
        self.assertEqual(op["method"], "GET")
        self.assertTrue(op.get("read_only", op.get("readOnly", False)))

    def test_write_catalog(self) -> None:
        """write_catalog should produce a file on disk."""
        app = App("test-plugin")

        @app.operation
        def noop() -> str:
            return "ok"

        with tempfile.TemporaryDirectory() as tmpdir:
            path = pathlib.Path(tmpdir) / "catalog.json"
            app.write_catalog(path)
            self.assertTrue(path.exists())
            content = path.read_text(encoding="utf-8")
            self.assertIn("test-plugin", content)

class AppNameTests(unittest.TestCase):
    """Tests for app name normalization."""

    def test_slug_normalization(self) -> None:
        """App names should be slugified."""
        app = App("My Cool Plugin!")
        self.assertEqual(app.name, "My-Cool-Plugin")

    def test_from_manifest_with_base_dir(self) -> None:
        """from_manifest with base_dir should resolve relative paths against it."""
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = pathlib.Path(tmpdir) / "manifest.yaml"
            manifest.write_text('display_name: "Test Plugin"\n', encoding="utf-8")

            app = App.from_manifest(
                "manifest.yaml", base_dir=pathlib.Path(tmpdir)
            )
            self.assertEqual(app.name, "Test-Plugin")


if __name__ == "__main__":
    unittest.main()
