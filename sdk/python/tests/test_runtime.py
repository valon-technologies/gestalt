from __future__ import annotations

import json
import pathlib
import tempfile
import unittest
from unittest import mock

from gestalt import Plugin
from gestalt import _bootstrap
from gestalt import _runtime


class RuntimeTests(unittest.TestCase):
    def test_main_loads_bundled_plugin_and_applies_plugin_name(self) -> None:
        plugin = Plugin("source-name")

        with tempfile.TemporaryDirectory() as tmpdir:
            bundle_dir = pathlib.Path(tmpdir)
            (bundle_dir / _bootstrap.BUNDLED_CONFIG_NAME).write_text(
                json.dumps(
                    {
                        "target": "provider:plugin",
                        "plugin_name": "released-plugin",
                    }
                ),
                encoding="utf-8",
            )

            with mock.patch.object(
                _runtime.sys,
                "_MEIPASS",
                str(bundle_dir),
                create=True,
            ), mock.patch.object(
                _runtime,
                "_load_plugin",
                return_value=plugin,
            ) as load_plugin, mock.patch.object(_runtime, "serve") as serve:
                result = _runtime.main([])

        self.assertEqual(result, 0)
        load_plugin.assert_called_once_with(
            _runtime.RuntimeArgs(
                target="provider:plugin",
                plugin_name="released-plugin",
            )
        )
        serve.assert_called_once_with(plugin)
        self.assertEqual(plugin.name, "released-plugin")

    def test_parse_runtime_args_accepts_explicit_root_and_target(self) -> None:
        runtime_args = _runtime._parse_runtime_args(
            ["/tmp/plugin", "example.plugin:PLUGIN"]
        )

        self.assertEqual(
            runtime_args,
            _runtime.RuntimeArgs(
                target="example.plugin:PLUGIN",
                root="/tmp/plugin",
            ),
        )

    def test_parse_runtime_args_rejects_invalid_explicit_arguments(self) -> None:
        self.assertIsNone(_runtime._parse_runtime_args(["/tmp/plugin"]))

    def test_write_catalog_if_requested_returns_false_without_env_var(self) -> None:
        plugin = mock.Mock(spec=Plugin)

        with mock.patch.dict(_runtime.os.environ, {}, clear=True):
            wrote_catalog = _runtime._write_catalog_if_requested(plugin)

        self.assertFalse(wrote_catalog)
        plugin.write_catalog.assert_not_called()

    def test_write_catalog_if_requested_writes_catalog_when_env_is_set(self) -> None:
        plugin = mock.Mock(spec=Plugin)

        with mock.patch.dict(
            _runtime.os.environ,
            {_runtime.ENV_WRITE_CATALOG: "/tmp/catalog.json"},
            clear=True,
        ):
            wrote_catalog = _runtime._write_catalog_if_requested(plugin)

        self.assertTrue(wrote_catalog)
        plugin.write_catalog.assert_called_once_with("/tmp/catalog.json")


if __name__ == "__main__":
    unittest.main()
