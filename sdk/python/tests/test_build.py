from __future__ import annotations

import dataclasses
import json
import pathlib
import tempfile
import unittest
from unittest import mock

from gestalt import _build


@dataclasses.dataclass(slots=True)
class CapturedBuildRun:
    command: list[str]
    cwd: pathlib.Path
    check: bool
    destination: str
    bundle_config: dict[str, str]


class BuildTests(unittest.TestCase):
    """Build entrypoint tests."""

    def test_build_plugin_binary_writes_bundle_config_and_uses_static_entrypoint(self) -> None:
        """Build mode should package runtime metadata alongside the frozen entrypoint."""
        with tempfile.TemporaryDirectory() as tmpdir:
            root = pathlib.Path(tmpdir) / "plugin"
            output = root / "dist" / "provider-bin"
            root.mkdir()
            captured: CapturedBuildRun | None = None

            def fake_run(command: list[str], cwd: pathlib.Path, check: bool) -> None:
                nonlocal captured

                add_data = command[command.index("--add-data") + 1]
                separator = _build.os.pathsep
                source, destination = add_data.split(separator, 1)
                captured = CapturedBuildRun(
                    command=command,
                    cwd=cwd,
                    check=check,
                    destination=destination,
                    bundle_config=json.loads(pathlib.Path(source).read_text(encoding="utf-8")),
                )

            with mock.patch.object(_build.subprocess, "run", side_effect=fake_run):
                _build.build_plugin_binary(
                    _build.BuildArgs(
                        root=root,
                        target="provider",
                        output_path=output,
                        plugin_name="released-plugin",
                    )
                )

        self.assertIsNotNone(captured)
        assert captured is not None
        self.assertIn("--add-data", captured.command)
        self.assertEqual(captured.cwd, root.resolve())
        self.assertTrue(captured.check)
        self.assertEqual(captured.destination, ".")
        self.assertEqual(
            captured.bundle_config,
            {
                "target": "provider",
                "plugin_name": "released-plugin",
            },
        )
        self.assertEqual(
            captured.command[-1],
            str(pathlib.Path(_build.__file__).with_name("_pyinstaller.py")),
        )


if __name__ == "__main__":
    unittest.main()
