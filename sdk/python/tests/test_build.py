import json
import pathlib
import tempfile
import unittest
from unittest import mock

from gestalt import _bootstrap
from gestalt import _build


class BuildTests(unittest.TestCase):
    """Build entrypoint tests."""

    def test_build_plugin_binary_writes_bundle_config_and_uses_static_entrypoint(self) -> None:
        """Build mode should package runtime metadata alongside the frozen entrypoint."""
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
                separator = ";" if _build.sys.platform == "win32" else ":"
                source, destination = add_data.split(separator, 1)
                captured["destination"] = destination
                captured["bundle_config"] = json.loads(pathlib.Path(source).read_text(encoding="utf-8"))

            with mock.patch.object(_build.subprocess, "run", side_effect=fake_run):
                _build.build_plugin_binary(
                    _build.BuildArgs(
                        root=str(root),
                        target="provider",
                        output_path=str(output),
                        plugin_name="released-plugin",
                    )
                )

        command = captured["command"]
        self.assertIsInstance(command, list)
        self.assertIn("--add-data", command)
        self.assertEqual(captured["cwd"], root.resolve())
        self.assertTrue(captured["check"])
        self.assertEqual(captured["destination"], ".")
        self.assertEqual(
            captured["bundle_config"],
            {
                "target": "provider",
                "plugin_name": "released-plugin",
            },
        )
        self.assertEqual(command[-1], str(pathlib.Path(_build.__file__).with_name("_pyinstaller.py")))


if __name__ == "__main__":
    unittest.main()
