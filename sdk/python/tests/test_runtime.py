import json
import pathlib
import tempfile
import unittest
from unittest import mock

from gestalt import Plugin
from gestalt import _bootstrap
from gestalt import _runtime


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
                        "target": "provider:plugin",
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
                target="provider:plugin",
                plugin_name="released-plugin",
            )
        )
        serve.assert_called_once_with(plugin)
        self.assertEqual(plugin.name, "released-plugin")


if __name__ == "__main__":
    unittest.main()
