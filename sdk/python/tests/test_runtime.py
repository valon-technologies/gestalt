from __future__ import annotations

import json
import pathlib
import tempfile
import unittest
from unittest import mock

from gestalt import Plugin
from gestalt import _runtime


class RuntimeTests(unittest.TestCase):
    def test_bundled_runtime_config_uses_bundled_metadata(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            bundle_dir = pathlib.Path(tmpdir)
            (bundle_dir / _runtime.BUNDLED_CONFIG_NAME).write_text(
                json.dumps(
                    {
                        "target": "provider:plugin",
                        "plugin_name": "released-plugin",
                    }
                ),
                encoding="utf-8",
            )

            with mock.patch.object(_runtime.sys, "_MEIPASS", str(bundle_dir), create=True):
                config = _runtime._bundled_runtime_config()

        self.assertEqual(
            config,
            ("provider:plugin", "released-plugin"),
        )

    def test_main_loads_bundled_plugin_and_applies_plugin_name(self) -> None:
        plugin = Plugin("source-name")

        with mock.patch.object(
            _runtime,
            "_bundled_runtime_config",
            return_value=("provider:plugin", "released-plugin"),
        ), mock.patch.object(_runtime, "_load_plugin", return_value=plugin) as load_plugin, mock.patch.object(
            _runtime, "serve"
        ) as serve:
            result = _runtime.main([])

        self.assertEqual(result, 0)
        load_plugin.assert_called_once_with("provider:plugin", None)
        serve.assert_called_once_with(plugin)
        self.assertEqual(plugin.name, "released-plugin")

    def test_build_plugin_binary_writes_bundle_config_and_uses_static_entrypoint(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            root = pathlib.Path(tmpdir) / "plugin"
            output = root / "dist" / "provider-bin"
            root.mkdir()
            captured: dict[str, object] = {}

            def fake_run(command: list[str], cwd: pathlib.Path, check: bool) -> None:
                captured["command"] = command
                captured["cwd"] = cwd
                captured["check"] = check

                add_data = command[command.index("--add-data") + 1]
                separator = ";" if _runtime.sys.platform == "win32" else ":"
                source, destination = add_data.split(separator, 1)
                captured["destination"] = destination
                captured["bundle_config"] = json.loads(pathlib.Path(source).read_text(encoding="utf-8"))

            with mock.patch.object(_runtime.subprocess, "run", side_effect=fake_run):
                _runtime.build_plugin_binary(
                    str(root),
                    "provider:plugin",
                    str(output),
                    "released-plugin",
                )

        command = captured["command"]
        self.assertIsInstance(command, list)
        self.assertIn("--add-data", command)
        self.assertEqual(captured["cwd"], root.resolve())
        self.assertTrue(captured["check"])
        self.assertEqual(captured["destination"], _runtime.BUNDLED_CONFIG_NAME)
        self.assertEqual(
            captured["bundle_config"],
            {
                "target": "provider:plugin",
                "plugin_name": "released-plugin",
            },
        )
        self.assertEqual(command[-1], str(_runtime._pyinstaller_entrypoint()))


if __name__ == "__main__":
    unittest.main()
