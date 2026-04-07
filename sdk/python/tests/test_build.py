import json
import pathlib
import tempfile
import unittest
from typing import Any
from unittest import mock

from gestalt import _build


class BuildTests(unittest.TestCase):
    """Build entrypoint tests."""

    def test_build_writes_config_and_invokes_pyinstaller(self) -> None:
        """Build should package runtime metadata alongside the frozen entrypoint."""
        cases = [
            ("linux", "provider-bin", "provider-bin"),
            ("linux", "provider.exe", "provider.exe"),
            ("win32", "provider.exe", "provider"),
        ]
        for platform, output_name, expected_binary_name in cases:
            with self.subTest(platform=platform, output_name=output_name):
                with tempfile.TemporaryDirectory() as tmpdir:
                    root = pathlib.Path(tmpdir) / "plugin"
                    output = root / "dist" / output_name
                    root.mkdir()

                    captured_config: dict[str, Any] = {}

                    def capture_run(command: list[str], **kwargs: Any) -> None:
                        add_data = command[command.index("--add-data") + 1]
                        source = add_data.split(_build.os.pathsep, 1)[0]
                        captured_config.update(
                            json.loads(pathlib.Path(source).read_text(encoding="utf-8"))
                        )

                    with mock.patch.object(_build.sys, "platform", platform), mock.patch.object(
                        _build.subprocess,
                        "run",
                        side_effect=capture_run,
                    ) as mock_run:
                        _build.build_plugin_binary(
                            _build.BuildArgs(
                                root=root,
                                target="provider",
                                output_path=output,
                                plugin_name="released-plugin",
                            )
                        )

                    mock_run.assert_called_once()
                    command = mock_run.call_args[0][0]
                    call_kwargs = mock_run.call_args[1]

                    self.assertEqual(call_kwargs["cwd"], root.resolve())
                    self.assertTrue(call_kwargs["check"])
                    self.assertEqual(command[command.index("--name") + 1], expected_binary_name)
                    self.assertEqual(
                        captured_config,
                        {"target": "provider", "plugin_name": "released-plugin"},
                    )
                    self.assertEqual(
                        command[-1],
                        str(pathlib.Path(_build.__file__).with_name("_pyinstaller.py")),
                    )

    def test_parse_build_args_rejects_wrong_count(self) -> None:
        """Build arg parser should reject incorrect argument counts."""
        self.assertIsNone(_build._parse_build_args([]))
        self.assertIsNone(_build._parse_build_args(["one"]))
        self.assertIsNone(_build._parse_build_args(["one", "two", "three"]))

    def test_parse_build_args_accepts_four_args(self) -> None:
        """Build arg parser should accept exactly four arguments."""
        result = _build._parse_build_args(["/root", "mod:attr", "/out/bin", "my-plugin"])

        self.assertIsNotNone(result)
        assert result is not None
        self.assertEqual(result.root, pathlib.Path("/root"))
        self.assertEqual(result.target, "mod:attr")
        self.assertEqual(result.output_path, pathlib.Path("/out/bin"))
        self.assertEqual(result.plugin_name, "my-plugin")


if __name__ == "__main__":
    unittest.main()
