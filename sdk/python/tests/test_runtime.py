import json
import pathlib
import tempfile
import unittest
from unittest import mock

from gestalt import Plugin, Request, _bootstrap, _runtime


class ParseRuntimeArgsTests(unittest.TestCase):
    """Tests for _parse_runtime_args, a pure function."""

    def test_explicit_root_and_target(self) -> None:
        """Explicit runtime invocation should keep the provided source root."""
        runtime_args = _runtime._parse_runtime_args(["/tmp/plugin", "example.plugin:PLUGIN"])

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(
                target="example.plugin:PLUGIN",
                root=pathlib.Path("/tmp/plugin"),
            ),
        )

    def test_rejects_single_argument(self) -> None:
        """Runtime invocation should reject incomplete explicit arguments."""
        self.assertIsNone(_runtime._parse_runtime_args(["/tmp/plugin"]))

    def test_bundled_config_fallback(self) -> None:
        """Empty args should fall back to bundled config when present."""
        with tempfile.TemporaryDirectory() as tmpdir:
            bundle_dir = pathlib.Path(tmpdir)
            (bundle_dir / _bootstrap.BUNDLED_CONFIG_NAME).write_text(
                json.dumps({"target": "provider", "plugin_name": "released-plugin"}),
                encoding="utf-8",
            )

            with mock.patch.object(_runtime.sys, "_MEIPASS", str(bundle_dir), create=True):
                runtime_args = _runtime._parse_runtime_args([])

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(target="provider", plugin_name="released-plugin"),
        )

    def test_returns_none_without_args_or_bundled_config(self) -> None:
        """Empty args without a bundled config should return None."""
        with tempfile.TemporaryDirectory() as tmpdir:
            with mock.patch.object(_runtime.sys, "_MEIPASS", tmpdir, create=True):
                self.assertIsNone(_runtime._parse_runtime_args([]))


class ManifestNameTests(unittest.TestCase):
    """Tests for manifest-derived plugin names."""

    def test_display_name_variants(self) -> None:
        """Manifest-derived plugins should normalize manifest names across formats."""
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

            tagged_manifest_path = temp_root / "tagged.yaml"
            tagged_manifest_path.write_text(
                "source: !env github.com/acme/plugins/tagged-provider\n"
                "display_name: !env ${PLUGIN_NAME}\n",
                encoding="utf-8",
            )

            cases = [
                (manifest_path, "Released-Plugin"),
                (manifest_dir, "Directory-Manifest"),
                (ascii_slug_manifest_path, "Cr-me-Br-l-e"),
                (tagged_manifest_path, "tagged-provider"),
            ]
            for manifest_input, expected_name in cases:
                with self.subTest(manifest_input=str(manifest_input)):
                    plugin = Plugin.from_manifest(manifest_input)
                    self.assertEqual(plugin.name, expected_name)


class RequestTests(unittest.TestCase):
    """Tests for the Request helper type."""

    def test_connection_param_returns_value_or_empty_string(self) -> None:
        """Request helpers should return the configured value or an empty string."""
        request = Request(connection_params={"region": "us-east-1"})

        self.assertEqual(request.connection_param("region"), "us-east-1")
        self.assertEqual(request.connection_param("missing"), "")


class MainEntrypointTests(unittest.TestCase):
    """Tests for the main() entrypoint, mocking only serve (gRPC)."""

    def test_writes_catalog_when_env_is_set(self) -> None:
        """Catalog export should write to the requested path when enabled."""
        plugin = Plugin("test-plugin")

        @plugin.operation
        def noop() -> str:
            return "ok"

        with tempfile.TemporaryDirectory() as tmpdir:
            catalog_path = pathlib.Path(tmpdir) / "catalog.yaml"
            with mock.patch.object(_runtime, "_load_plugin", return_value=plugin), mock.patch.dict(
                _runtime.os.environ,
                {_runtime.ENV_WRITE_CATALOG: str(catalog_path)},
                clear=True,
            ):
                result = _runtime.main(["/tmp/plugin", "example.plugin:PLUGIN"])

            self.assertEqual(result, 0)
            self.assertTrue(catalog_path.exists())

    def test_returns_usage_error_for_bad_args(self) -> None:
        """Invalid args should return exit code 2."""
        result = _runtime.main(["/only-one-arg"])
        self.assertEqual(result, 2)


if __name__ == "__main__":
    unittest.main()
