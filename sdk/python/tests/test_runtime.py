from __future__ import annotations

import contextlib
import io
import json
import pathlib
import tempfile
import unittest
from unittest import mock

from gestalt import Plugin, Request, _bootstrap, _runtime


class RuntimeTests(unittest.TestCase):
    """Runtime entrypoint tests."""

    def test_main_loads_bundled_plugin_and_applies_plugin_name(self) -> None:
        """Bundled executions should load target metadata from the packaged config."""
        plugin = Plugin("source-name")

        with tempfile.TemporaryDirectory() as tmpdir:
            bundle_dir = pathlib.Path(tmpdir)
            (bundle_dir / _bootstrap.BUNDLED_CONFIG_NAME).write_text(
                json.dumps(
                    {
                        "target": "provider",
                        "plugin_name": "released-plugin",
                    }
                ),
                encoding="utf-8",
            )

            with mock.patch.object(_runtime.sys, "_MEIPASS", str(bundle_dir), create=True), mock.patch.object(
                _runtime,
                "_load_plugin",
                return_value=plugin,
            ) as load_plugin, mock.patch.object(_runtime, "serve") as serve:
                result = _runtime.main([])

        self.assertEqual(result, 0)
        load_plugin.assert_called_once_with(
            _runtime.RuntimeArgs(
                target="provider",
                plugin_name="released-plugin",
            )
        )
        serve.assert_called_once_with(plugin)
        self.assertEqual(plugin.name, "released-plugin")

    def test_parse_runtime_args_accepts_explicit_root_and_target(self) -> None:
        """Explicit runtime invocation should keep the provided source root."""
        runtime_args = _runtime._parse_runtime_args(["/tmp/plugin", "example.plugin:PLUGIN"])

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(
                target="example.plugin:PLUGIN",
                root=pathlib.Path("/tmp/plugin"),
            ),
        )

    def test_parse_runtime_args_rejects_invalid_explicit_arguments(self) -> None:
        """Runtime invocation should reject incomplete explicit arguments."""
        self.assertIsNone(_runtime._parse_runtime_args(["/tmp/plugin"]))

    def test_main_skips_catalog_export_without_env_var(self) -> None:
        """Catalog export should be skipped when the request env var is absent."""
        plugin = mock.Mock(spec=Plugin)

        with mock.patch.object(_runtime, "_load_plugin", return_value=plugin), mock.patch.object(
            _runtime, "serve"
        ) as serve, mock.patch.dict(_runtime.os.environ, {}, clear=True):
            result = _runtime.main(["/tmp/plugin", "example.plugin:PLUGIN"])

        self.assertEqual(result, 0)
        plugin.write_catalog.assert_not_called()
        serve.assert_called_once_with(plugin)

    def test_main_writes_catalog_when_env_is_set(self) -> None:
        """Catalog export should write to the requested path when enabled."""
        plugin = mock.Mock(spec=Plugin)

        with mock.patch.object(_runtime, "_load_plugin", return_value=plugin), mock.patch.object(
            _runtime, "serve"
        ) as serve, mock.patch.dict(
            _runtime.os.environ,
            {_runtime.ENV_WRITE_CATALOG: "/tmp/catalog.json"},
            clear=True,
        ):
            result = _runtime.main(["/tmp/plugin", "example.plugin:PLUGIN"])

        self.assertEqual(result, 0)
        plugin.write_catalog.assert_called_once_with("/tmp/catalog.json")
        serve.assert_not_called()

    def test_plugin_from_manifest_uses_display_name(self) -> None:
        """Manifest-derived plugins should normalize manifest names across path layouts."""
        with tempfile.TemporaryDirectory() as tmpdir:
            temp_root = pathlib.Path(tmpdir)
            manifest_path = temp_root / "plugin.yaml"
            manifest_path.write_text('display_name: "Released Plugin"\n', encoding="utf-8")

            manifest_dir = temp_root / "plugin.json"
            manifest_dir.mkdir()
            (manifest_dir / "plugin.yaml").write_text(
                'display_name: "Directory Manifest"\n',
                encoding="utf-8",
            )

            ascii_slug_manifest_path = temp_root / "ascii-slug.yaml"
            ascii_slug_manifest_path.write_text(
                'display_name: "Crème Brûlée"\n',
                encoding="utf-8",
            )

            cases = [
                (manifest_path, "Released-Plugin"),
                (manifest_dir, "Directory-Manifest"),
                (ascii_slug_manifest_path, "Cr-me-Br-l-e"),
            ]
            for manifest_input, expected_name in cases:
                with self.subTest(manifest_input=str(manifest_input)):
                    plugin = Plugin.from_manifest(manifest_input)
                    self.assertEqual(plugin.name, expected_name)

    def test_request_connection_param_returns_empty_string_when_missing(self) -> None:
        """Request helpers should return the configured value or an empty string."""
        request = Request(connection_params={"region": "us-east-1"})

        self.assertEqual(request.connection_param("region"), "us-east-1")
        self.assertEqual(request.connection_param("missing"), "")

    def test_plugin_execute_returns_http_results_for_operation_outcomes(self) -> None:
        """Plugin execution should map missing, successful, and unserializable results to HTTP-style responses."""
        plugin = Plugin("source-name")

        @plugin.operation
        def ok() -> dict[str, str]:
            return {"status": "ok"}

        @plugin.operation
        def broken() -> object:
            return object()

        cases = [
            ("ok", 200, {"status": "ok"}, None),
            ("missing", 404, None, "unknown operation"),
            ("broken", 500, None, "not JSON serializable"),
        ]
        for operation_name, expected_status, expected_body, expected_error in cases:
            with self.subTest(operation_name=operation_name):
                stderr_buffer = io.StringIO()
                with contextlib.redirect_stderr(stderr_buffer):
                    status, body = plugin.execute(operation_name, {}, Request())

                self.assertEqual(status, expected_status)
                payload = json.loads(body)
                if expected_body is not None:
                    self.assertEqual(payload, expected_body)
                if expected_error is not None:
                    self.assertIn(expected_error, payload["error"])


if __name__ == "__main__":
    unittest.main()
