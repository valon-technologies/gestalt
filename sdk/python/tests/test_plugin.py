import json
import pathlib
import tempfile
import unittest
from dataclasses import dataclass

from gestalt import OK, Plugin, Request, Response


class PluginOperationTests(unittest.TestCase):
    """Tests for Plugin operation registration and execution using real handlers."""

    def test_register_and_execute_operation(self) -> None:
        """Registering an operation and executing it should return the handler's result."""
        plugin = Plugin("test-plugin")

        @plugin.operation
        def greet() -> dict[str, str]:
            return {"message": "hello"}

        result = plugin.execute("greet", {}, Request())
        self.assertEqual(result.status, 200)
        self.assertEqual(json.loads(result.body), {"message": "hello"})

    def test_execute_missing_operation(self) -> None:
        """Executing a non-existent operation should return 404."""
        plugin = Plugin("test-plugin")

        result = plugin.execute("missing", {}, Request())
        self.assertEqual(result.status, 404)

    def test_operation_with_input(self) -> None:
        """Operations with typed input should decode params correctly."""
        plugin = Plugin("test-plugin")

        @dataclass
        class Input:
            name: str
            count: int = 1

        @plugin.operation
        def greet(input: Input) -> dict[str, str]:
            return {"message": f"hello {input.name} x{input.count}"}

        result = plugin.execute("greet", {"name": "world", "count": 3}, Request())
        self.assertEqual(result.status, 200)
        body = json.loads(result.body)
        self.assertEqual(body["message"], "hello world x3")

    def test_operation_with_response_wrapper(self) -> None:
        """Operations returning Response should preserve status and body."""
        plugin = Plugin("test-plugin")

        @plugin.operation
        def created() -> Response[dict[str, str]]:
            return Response(status=201, body={"id": "abc"})

        result = plugin.execute("created", {}, Request())
        self.assertEqual(result.status, 201)
        self.assertEqual(json.loads(result.body), {"id": "abc"})

    def test_ok_helper(self) -> None:
        """The OK() helper should produce status 200."""
        plugin = Plugin("test-plugin")

        @plugin.operation
        def ok_op() -> Response[str]:
            return OK("done")

        result = plugin.execute("ok_op", {}, Request())
        self.assertEqual(result.status, 200)

    def test_operation_with_custom_id(self) -> None:
        """Operations can specify a custom ID separate from the function name."""
        plugin = Plugin("test-plugin")

        @plugin.operation(id="custom-id", method="GET")
        def handler() -> str:
            return "ok"

        result = plugin.execute("custom-id", {}, Request())
        self.assertEqual(result.status, 200)

    def test_duplicate_operation_id_raises(self) -> None:
        """Registering two operations with the same ID should raise."""
        plugin = Plugin("test-plugin")

        @plugin.operation(id="dup")
        def first() -> str:
            return "first"

        with self.assertRaises(ValueError, msg="duplicate operation id"):
            @plugin.operation(id="dup")
            def second() -> str:
                return "second"

    def test_handler_receives_request(self) -> None:
        """Operations that take Request should receive it with token and params."""
        plugin = Plugin("test-plugin")

        @plugin.operation
        def echo(req: Request) -> dict[str, str]:
            return {"token": req.token, "region": req.connection_param("region")}

        result = plugin.execute(
            "echo",
            {},
            Request(token="tok-abc", connection_params={"region": "us-east-1"}),
        )
        body = json.loads(result.body)
        self.assertEqual(body["token"], "tok-abc")
        self.assertEqual(body["region"], "us-east-1")

    def test_handler_exception_returns_500(self) -> None:
        """A handler that raises should return 500 with the error message."""
        plugin = Plugin("test-plugin")

        @plugin.operation
        def broken() -> None:
            raise RuntimeError("something broke")

        result = plugin.execute("broken", {}, Request())
        self.assertEqual(result.status, 500)
        self.assertIn("something broke", json.loads(result.body)["error"])


class PluginConfigureTests(unittest.TestCase):
    """Tests for the @plugin.configure decorator."""

    def test_configure_handler_called(self) -> None:
        """The configure handler should be called with name and config."""
        plugin = Plugin("test-plugin")
        calls: list[tuple[str, dict[str, str]]] = []

        @plugin.configure
        def setup(name: str, config: dict[str, str]) -> None:
            calls.append((name, config))

        plugin.configure_provider("my-provider", {"key": "value"})

        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0], ("my-provider", {"key": "value"}))

    def test_no_configure_handler_is_noop(self) -> None:
        """Without a configure handler, configure_provider should be a no-op."""
        plugin = Plugin("test-plugin")
        plugin.configure_provider("my-provider", {"key": "value"})


class PluginCatalogTests(unittest.TestCase):
    """Tests for catalog generation."""

    def test_catalog_dict(self) -> None:
        """catalog_dict should return the plugin name and operation list."""
        plugin = Plugin("test-plugin")

        @plugin.operation(method="GET", description="Say hello", read_only=True)
        def greet() -> str:
            return "hello"

        catalog = plugin.catalog_dict()
        self.assertEqual(catalog["name"], "test-plugin")
        self.assertEqual(len(catalog["operations"]), 1)
        self.assertEqual(catalog["operations"][0]["id"], "greet")
        self.assertEqual(catalog["operations"][0]["method"], "GET")
        self.assertTrue(catalog["operations"][0]["read_only"])

    def test_write_catalog(self) -> None:
        """write_catalog should produce a YAML file on disk."""
        plugin = Plugin("test-plugin")

        @plugin.operation
        def noop() -> str:
            return "ok"

        with tempfile.TemporaryDirectory() as tmpdir:
            path = pathlib.Path(tmpdir) / "catalog.yaml"
            plugin.write_catalog(path)
            self.assertTrue(path.exists())
            content = path.read_text(encoding="utf-8")
            self.assertIn("test-plugin", content)


class PluginNameTests(unittest.TestCase):
    """Tests for plugin name normalization."""

    def test_slug_normalization(self) -> None:
        """Plugin names should be slugified."""
        plugin = Plugin("My Cool Plugin!")
        self.assertEqual(plugin.name, "My-Cool-Plugin")

    def test_from_manifest_with_base_dir(self) -> None:
        """from_manifest with base_dir should resolve relative paths against it."""
        with tempfile.TemporaryDirectory() as tmpdir:
            manifest = pathlib.Path(tmpdir) / "plugin.yaml"
            manifest.write_text('display_name: "Test Plugin"\n', encoding="utf-8")

            plugin = Plugin.from_manifest("plugin.yaml", base_dir=pathlib.Path(tmpdir))
            self.assertEqual(plugin.name, "Test-Plugin")


if __name__ == "__main__":
    unittest.main()
