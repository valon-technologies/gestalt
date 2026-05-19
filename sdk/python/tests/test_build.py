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
    pyinstaller_config_dir: pathlib.Path
    work_path: pathlib.Path
    spec_path: pathlib.Path
    add_data_source: pathlib.Path
    destination: str
    bundle_config: dict[str, str]


class BuildTests(unittest.TestCase):
    """Build entrypoint tests."""

    def capture_build_run(
        self,
        *,
        root: pathlib.Path,
        output: pathlib.Path,
        platform_name: str = "linux",
        target: str = "provider",
        plugin_name: str = "released-plugin",
        runtime_kind: str = "integration",
        goos: str = "linux",
        goarch: str = "amd64",
    ) -> CapturedBuildRun:
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
                pyinstaller_config_dir=pathlib.Path(env["PYINSTALLER_CONFIG_DIR"]),
                work_path=pathlib.Path(command[command.index("--workpath") + 1]),
                spec_path=pathlib.Path(command[command.index("--specpath") + 1]),
                add_data_source=pathlib.Path(source),
                destination=destination,
                bundle_config=json.loads(pathlib.Path(source).read_text(encoding="utf-8")),
            )

        with (
            mock.patch.object(_build.sys, "platform", platform_name),
            mock.patch.object(
                _build.subprocess,
                "run",
                side_effect=fake_run,
            ),
        ):
            _build.build_plugin_binary(
                _build.BuildArgs(
                    root=root,
                    target=target,
                    output_path=output,
                    plugin_name=plugin_name,
                    runtime_kind=runtime_kind,
                    goos=goos,
                    goarch=goarch,
                )
            )

        self.assertIsNotNone(captured)
        assert captured is not None
        return captured

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
                    with mock.patch.dict(_build.os.environ, {}, clear=False):
                        _build.os.environ.pop(_build.BUILD_CACHE_DIR_ENV_VAR, None)
                        captured = self.capture_build_run(
                            root=root,
                            output=output,
                            platform_name=platform,
                            goos=target_goos,
                            goarch=target_goarch,
                        )

                self.assertIn("--add-data", captured.command)
                self.assertIn("--clean", captured.command)
                self.assertIn("--noupx", captured.command)
                self.assertEqual(captured.cwd, root.resolve())
                self.assertTrue(captured.check)
                self.assertEqual(captured.binary_name, expected_binary_name)
                self.assertEqual(captured.target_arch, expected_target_arch)
                self.assertEqual(captured.destination, ".")
                self.assertEqual(
                    captured.pyinstaller_config_dir.name,
                    "pyinstaller-config",
                )
                self.assertEqual(captured.env["SOURCE_DATE_EPOCH"], "0")
                self.assertIn("--collect-submodules", captured.command)
                self.assertEqual(
                    captured.command[
                        captured.command.index("--collect-submodules") + 1
                    ],
                    "gestalt",
                )
                self.assertEqual(
                    captured.command[captured.command.index("--hidden-import") + 1],
                    "provider",
                )
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

    def test_build_plugin_binary_uses_generic_build_cache_when_configured(self) -> None:
        """Cache mode should persist only private packager cache state."""
        with tempfile.TemporaryDirectory() as tmpdir:
            base = pathlib.Path(tmpdir)
            root = base / "plugin"
            output = root / "dist" / "provider-bin"
            cache_root = base / "cache"
            root.mkdir()

            with mock.patch.dict(
                _build.os.environ,
                {_build.BUILD_CACHE_DIR_ENV_VAR: str(cache_root)},
                clear=False,
            ):
                captured = self.capture_build_run(root=root, output=output)

            resolved_cache_root = cache_root.resolve()
            self.assertNotIn("--clean", captured.command)
            self.assertTrue(captured.pyinstaller_config_dir.is_relative_to(resolved_cache_root))
            self.assertEqual(captured.pyinstaller_config_dir.name, "pyinstaller-config")
            self.assertTrue((captured.pyinstaller_config_dir.parent / "build.lock").exists())
            self.assertFalse(captured.work_path.is_relative_to(resolved_cache_root))
            self.assertFalse(captured.spec_path.is_relative_to(resolved_cache_root))
            self.assertFalse(captured.add_data_source.is_relative_to(resolved_cache_root))
            self.assertEqual(
                captured.bundle_config,
                {
                    "target": "provider",
                    "plugin_name": "released-plugin",
                    "runtime_kind": "integration",
                },
            )

    def test_build_cache_namespace_separates_source_target_and_platform(self) -> None:
        """Distinct build domains should not share private packager cache dirs."""
        with tempfile.TemporaryDirectory() as tmpdir:
            base = pathlib.Path(tmpdir)
            cache_root = base / "cache"
            root_a = base / "plugin-a"
            root_b = base / "plugin-b"
            root_a.mkdir()
            root_b.mkdir()

            with mock.patch.dict(
                _build.os.environ,
                {_build.BUILD_CACHE_DIR_ENV_VAR: str(cache_root)},
                clear=False,
            ):
                source_a = self.capture_build_run(
                    root=root_a,
                    output=root_a / "dist" / "provider-bin",
                )
                source_b = self.capture_build_run(
                    root=root_b,
                    output=root_b / "dist" / "provider-bin",
                )
                target_b = self.capture_build_run(
                    root=root_a,
                    output=root_a / "dist" / "provider-other",
                    target="other_provider",
                )
                platform_b = self.capture_build_run(
                    root=root_a,
                    output=root_a / "dist" / "provider-arm64",
                    goarch="arm64",
                )

            cache_dirs = {
                source_a.pyinstaller_config_dir.parent,
                source_b.pyinstaller_config_dir.parent,
                target_b.pyinstaller_config_dir.parent,
                platform_b.pyinstaller_config_dir.parent,
            }
            self.assertEqual(len(cache_dirs), 4)

    def test_whitespace_build_cache_dir_keeps_default_clean_build(self) -> None:
        """Blank cache env values should behave like unset env values."""
        with tempfile.TemporaryDirectory() as tmpdir:
            root = pathlib.Path(tmpdir) / "plugin"
            output = root / "dist" / "provider-bin"
            root.mkdir()

            with mock.patch.dict(
                _build.os.environ,
                {_build.BUILD_CACHE_DIR_ENV_VAR: "   "},
                clear=False,
            ):
                captured = self.capture_build_run(root=root, output=output)

            self.assertIn("--clean", captured.command)
            self.assertEqual(captured.pyinstaller_config_dir.name, "pyinstaller-config")

    def test_build_cache_dir_file_fails_before_packager_invocation(self) -> None:
        """A non-directory cache path should fail with a clear SDK error."""
        with tempfile.TemporaryDirectory() as tmpdir:
            root = pathlib.Path(tmpdir) / "plugin"
            output = root / "dist" / "provider-bin"
            cache_file = pathlib.Path(tmpdir) / "cache-file"
            root.mkdir()
            cache_file.write_text("not a directory", encoding="utf-8")

            with (
                mock.patch.dict(
                    _build.os.environ,
                    {_build.BUILD_CACHE_DIR_ENV_VAR: str(cache_file)},
                    clear=False,
                ),
                mock.patch.object(_build.subprocess, "run") as run_mock,
            ):
                with self.assertRaisesRegex(RuntimeError, _build.BUILD_CACHE_DIR_ENV_VAR):
                    _build.build_plugin_binary(
                        _build.BuildArgs(
                            root=root,
                            target="provider",
                            output_path=output,
                            plugin_name="released-plugin",
                            runtime_kind="integration",
                            goos="linux",
                            goarch="amd64",
                        )
                    )
            run_mock.assert_not_called()

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
