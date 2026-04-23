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
    env: dict[str, str]
    binary_name: str
    target_arch: str | None
    destination: str
    bundle_config: dict[str, str]


class BuildTests(unittest.TestCase):
    """Build entrypoint tests."""

    def test_build_plugin_binary_writes_bundle_config_and_uses_target_platform_settings(
        self,
    ) -> None:
        """Build mode should package runtime metadata alongside the frozen entrypoint."""
        cases = [
            ("linux", "linux", "amd64", "provider-bin", "provider-bin", None),
            ("linux", "windows", "amd64", "provider.exe", "provider", None),
            ("darwin", "darwin", "amd64", "provider-bin", "provider-bin", "x86_64"),
        ]
        for (
            platform,
            target_goos,
            target_goarch,
            output_name,
            expected_binary_name,
            expected_target_arch,
        ) in cases:
            with self.subTest(
                platform=platform,
                target_goos=target_goos,
                target_goarch=target_goarch,
                output_name=output_name,
            ):
                with tempfile.TemporaryDirectory() as tmpdir:
                    root = pathlib.Path(tmpdir) / "plugin"
                    output = root / "dist" / output_name
                    root.mkdir()
                    captured: CapturedBuildRun | None = None

                    def fake_run(
                        command: list[str],
                        cwd: pathlib.Path,
                        env: dict[str, str],
                        check: bool,
                    ) -> None:
                        nonlocal captured

                        add_data = command[command.index("--add-data") + 1]
                        separator = _build.os.pathsep
                        source, destination = add_data.split(separator, 1)
                        captured = CapturedBuildRun(
                            command=command,
                            cwd=cwd,
                            check=check,
                            env=env,
                            binary_name=command[command.index("--name") + 1],
                            target_arch=command[command.index("--target-arch") + 1]
                            if "--target-arch" in command
                            else None,
                            destination=destination,
                            bundle_config=json.loads(
                                pathlib.Path(source).read_text(encoding="utf-8")
                            ),
                        )

                    with (
                        mock.patch.object(_build.sys, "platform", platform),
                        mock.patch.object(
                            _build.subprocess,
                            "run",
                            side_effect=fake_run,
                        ),
                    ):
                        _build.build_plugin_binary(
                            _build.BuildArgs(
                                root=root,
                                target="provider",
                                output_path=output,
                                plugin_name="released-plugin",
                                runtime_kind="integration",
                                goos=target_goos,
                                goarch=target_goarch,
                            )
                        )

                self.assertIsNotNone(captured)
                assert captured is not None
                self.assertIn("--add-data", captured.command)
                self.assertEqual(captured.cwd, root.resolve())
                self.assertTrue(captured.check)
                self.assertEqual(captured.binary_name, expected_binary_name)
                self.assertEqual(captured.target_arch, expected_target_arch)
                self.assertEqual(captured.destination, ".")
                self.assertEqual(
                    pathlib.Path(captured.env["PYINSTALLER_CONFIG_DIR"]).name,
                    "pyinstaller-config",
                )
                self.assertEqual(captured.env["SOURCE_DATE_EPOCH"], "0")
                self.assertEqual(
                    captured.bundle_config,
                    {
                        "target": "provider",
                        "plugin_name": "released-plugin",
                        "runtime_kind": "integration",
                    },
                )
                self.assertIn(
                    str(pathlib.Path(_build.__file__).with_name("_pyinstaller.py")),
                    captured.command,
                )

    def test_parse_build_args_rejects_wrong_count(self) -> None:
        """Build arg parser should reject incorrect argument counts."""
        self.assertIsNone(_build._parse_build_args([]))
        self.assertIsNone(_build._parse_build_args(["one"]))
        self.assertIsNone(_build._parse_build_args(["one", "two", "three"]))
        self.assertIsNone(
            _build._parse_build_args(["one", "two", "three", "four", "five"])
        )
        self.assertIsNone(
            _build._parse_build_args(["one", "two", "three", "four", "five", "six"])
        )

    def test_parse_build_args_accepts_seven_args(self) -> None:
        """Build arg parser should accept the release build argument list."""
        result = _build._parse_build_args(
            [
                "/root",
                "mod:attr",
                "/out/bin",
                "my-plugin",
                "integration",
                "linux",
                "amd64",
            ]
        )

        self.assertIsNotNone(result)
        assert result is not None
        self.assertEqual(result.root, pathlib.Path("/root"))
        self.assertEqual(result.target, "mod:attr")
        self.assertEqual(result.output_path, pathlib.Path("/out/bin"))
        self.assertEqual(result.plugin_name, "my-plugin")
        self.assertEqual(result.runtime_kind, "integration")
        self.assertEqual(result.goos, "linux")
        self.assertEqual(result.goarch, "amd64")


if __name__ == "__main__":
    unittest.main()
