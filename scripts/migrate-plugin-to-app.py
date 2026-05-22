#!/usr/bin/env python3
"""Mechanical plugin→app rename for gestalt monorepo. Run from repo root."""

from __future__ import annotations

import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

SKIP_DIRS = {
    ".git",
    "node_modules",
    "vendor",
    "out",
    "dist",
    ".next",
    "__pycache__",
    "target",
}

SKIP_FILE_SUFFIXES = {".png", ".jpg", ".svg", ".ico", ".woff", ".woff2", ".tar.gz", ".lock"}

# Files where top-level `apps:` is buf codegen config, not gestalt config.
BUF_GEN_YAML = {
    "buf.typescript.gen.yaml",
    "buf.python.gen.yaml",
    "buf.rust.gen.yaml",
    "buf.go.server.gen.yaml",
    "buf.go.sdk.gen.yaml",
}

# Ordered replacements: longest/most-specific first.
REPLACEMENTS = [
    ("AppRuntimeEgressMode", "AppRuntimeEgressMode"),
    ("APP_RUNTIME_EGRESS_MODE", "APP_RUNTIME_EGRESS_MODE"),
    ("defineAppRuntimeProvider", "defineAppRuntimeProvider"),
    ("AppRuntimeLogHost", "AppRuntimeLogHost"),
    ("AppRuntimeProvider", "AppRuntimeProvider"),
    ("AppRuntimeSessionLifecycle", "AppRuntimeSessionLifecycle"),
    ("AppRuntimeImagePullAuth", "AppRuntimeImagePullAuth"),
    ("StartAppRuntimeSessionRequest", "StartAppRuntimeSessionRequest"),
    ("ListAppRuntimeSessionsRequest", "ListAppRuntimeSessionsRequest"),
    ("ListAppRuntimeSessionsResponse", "ListAppRuntimeSessionsResponse"),
    ("GetAppRuntimeSessionRequest", "GetAppRuntimeSessionRequest"),
    ("StopAppRuntimeSessionRequest", "StopAppRuntimeSessionRequest"),
    ("PrepareAppRuntimeWorkspaceRequest", "PrepareAppRuntimeWorkspaceRequest"),
    ("PrepareAppRuntimeWorkspaceResponse", "PrepareAppRuntimeWorkspaceResponse"),
    ("RemoveAppRuntimeWorkspaceRequest", "RemoveAppRuntimeWorkspaceRequest"),
    ("AppendAppRuntimeLogsRequest", "AppendAppRuntimeLogsRequest"),
    ("AppendAppRuntimeLogsResponse", "AppendAppRuntimeLogsResponse"),
    ("AppRuntimeLogEntry", "AppRuntimeLogEntry"),
    ("AppRuntimeLogStream", "AppRuntimeLogStream"),
    ("StartAppRuntimeSession", "StartAppRuntimeSession"),
    ("AppRuntimeSession", "AppRuntimeSession"),
    ("AppRuntimeSupport", "AppRuntimeSupport"),
    ("StartHostedAppRequest", "StartHostedAppRequest"),
    ("BoundWorkflowAppTarget", "BoundWorkflowAppTarget"),
    ("AppInvocationGrant", "AppInvocationGrant"),
    ("AppInvokeGraphQLRequest", "AppInvokeGraphQLRequest"),
    ("AppInvokeRequest", "AppInvokeRequest"),
    ("AppInvocationDependency", "AppInvocationDependency"),
    ("AppInvocationGrants", "AppInvocationGrants"),
    ("AppInvokerServer", "AppInvokerServer"),
    ("AppInvokerEnv", "AppInvokerEnv"),
    ("AppInvokerClient", "AppInvokerClient"),
    ("AppInvoker", "AppInvoker"),
    ("AppProvider", "AppProvider"),
    ("AppRuntime", "AppRuntime"),
    ("HostedApp", "HostedApp"),
    ("AppValidationConfig", "AppValidationConfig"),
    ("ValidationApp", "ValidationApp"),
    ("AppValidationEntry", "AppValidationEntry"),
    ("EffectiveAppIndexedDB", "EffectiveAppIndexedDB"),
    ("ResolveEffectiveAppIndexedDB", "ResolveEffectiveAppIndexedDB"),
    ("ApplyAppScope", "ApplyAppScope"),
    ("NormalizeAppScopeNames", "NormalizeAppScopeNames"),
    ("AppScope", "AppScope"),
    ("AppCapabilitiesConfig", "AppCapabilitiesConfig"),
    ("AppInvocation", "AppInvocation"),
    ("decodeAppInvocationGrantProto", "decodeAppInvocationGrantProto"),
    ("NewAppInvokerServer", "NewAppInvokerServer"),
    ("AppRuntimePlan", "AppRuntimePlan"),
    ("AppRuntimeLaunch", "AppRuntimeLaunch"),
    ("AppRuntimeInspect", "AppRuntimeInspect"),
    ("AppRuntimeExecutable", "AppRuntimeExecutable"),
    ("app_runtime_plan", "app_runtime_plan"),
    ("app_runtime_launch", "app_runtime_launch"),
    ("app_runtime_inspect", "app_runtime_inspect"),
    ("app_runtime_executable", "app_runtime_executable"),
    ("app_runtime", "app_runtime"),
    ("app_invoker", "app_invoker"),
    ("app_invocation", "app_invocation"),
    ("app_validation", "app_validation"),
    ("app_scope", "app_scope"),
    ("appinvoker", "appinvoker"),
    ("appruntime", "appruntime"),
    ("can_host_apps", "can_host_apps"),
    ("caller_app_name", "caller_app_name"),
    ("target_app", "target_app"),
    ("app_name", "app_name"),
    ("AppName", "AppName"),
    ("owner_app", "owner_app"),
    ("ownerApp", "ownerApp"),
    ("OwnerApp", "OwnerApp"),
    ("by_app", "by_app"),
    ("app_static", "app_static"),
    ("app_dynamic", "app_dynamic"),
    ("AuthorizationFragmentOwnerKindApp", "AuthorizationFragmentOwnerKindApp"),
    ("legacy_app_dynamic", "legacy_app_dynamic"),
    ("PROVIDER_KIND_APP", "PROVIDER_KIND_APP"),
    ("ProviderKindApp", "ProviderKindApp"),
    ("ProviderKind_App", "ProviderKind_App"),
    ("PROVIDER_KIND_APP", "PROVIDER_KIND_APP"),
    ("defineApp", "defineApp"),
    ("KindApp", "KindApp"),
    ("gestalt-app-", "gestalt-app-"),
    ("provider_app", "provider_app"),
    ("sdkapp", "sdkapp"),
    ("testutil/sdkapp", "testutil/sdkapp"),
    ("APP_CONNECTION", "APP_CONNECTION"),
    ("APP_LIST_HEADERS", "APP_LIST_HEADERS"),
    ("APP_GROUP_DIVIDER", "APP_GROUP_DIVIDER"),
    ("app_group_divider", "app_group_divider"),
    ("app_rows", "app_rows"),
    ("print_app_list", "print_app_list"),
    ("render_app_list", "render_app_list"),
    ("filter_by_app", "filter_by_app"),
    ("list_run_target_apps", "list_run_target_apps"),
    ("buildAppStaticSpec", "buildAppStaticSpec"),
    ("AppCommands", "AppCommands"),
    ("app_errors", "app_errors"),
    ("apps_connect", "apps_connect"),
    ("AppProvider", "AppProvider"),
    ("AppRuntimeProvider", "AppRuntimeProvider"),
    ("AppRuntimeEgressMode", "AppRuntimeEgressMode"),
    ("gestaltd.config/v6", "gestaltd.config/v6"),
    ("providerLockLegacySchemaVersion = 7", "providerLockLegacySchemaVersion = 7"),
    ("providerLockSchemaVersion       = 8", "providerLockSchemaVersion       = 8"),
    ("providerLockSchemaVersion = 8", "providerLockSchemaVersion = 8"),
    ('"app"', '"app"'),
    ("'app'", "'app'"),
    ("v1/app.proto", "v1/app.proto"),
    ("v1/appruntime.proto", "v1/appruntime.proto"),
    ("app_pb", "app_pb"),
    ("appruntime_pb", "appruntime_pb"),
    ("_app.py", "_app.py"),
    ("_appruntime.py", "_appruntime.py"),
    ("app.ts", "app.ts"),
    ("appruntime.ts", "appruntime.ts"),
    ("app.rs", "app.rs"),
    ("app_runtime.rs", "app_runtime.rs"),
    ("serve_appruntime.go", "serve_appruntime.go"),
    ("appruntime_provider.go", "appruntime_provider.go"),
    ("appruntime_transport_test.go", "appruntime_transport_test.go"),
    ("app_invoker_transport", "app_invoker_transport"),
    ("app_runtime_transport", "app_runtime_transport"),
    ("test_app.py", "test_app.py"),
    ("test_app_invoker", "test_app_invoker"),
    ("/api/v1/apps", "/api/v1/apps"),
    ("/authorization/apps", "/authorization/apps"),
    ("/apps", "/apps"),
    ("apps.mdx", "apps.mdx"),
    ("app-manifests.mdx", "app-manifests.mdx"),
    ("solutions.mdx", "solutions.mdx"),
    ("/providers/apps", "/providers/apps"),
    ("/providers/app/", "/providers/app/"),
    ("/reference/app-manifests", "/reference/app-manifests"),
    ("apps/github", "apps/github"),
    ("apps/slack", "apps/slack"),
    ("gestalt-providers/apps/", "gestalt-providers/apps/"),
    ("valon-tools/apps/", "valon-tools/apps/"),
    ("github.com/acme/apps/", "github.com/acme/apps/"),
    ("services/apps/", "services/apps/"),
    ("services/appinvoker/", "services/appinvoker/"),
    ("runtimehost/appruntime/", "runtimehost/appruntime/"),
    ("internal/bootstrap/app_runtime", "internal/bootstrap/app_runtime"),
    ("internal/config/app_", "internal/config/app_"),
    ("internal/testutil/sdkapp", "internal/testutil/sdkapp"),
    ("Config.Apps", "Config.Apps"),
    ("cfg.Apps", "cfg.Apps"),
    ("ValidationConfig.Apps", "ValidationConfig.Apps"),
    ("providerLockBuckets.App", "providerLockBuckets.App"),
    ('json:"app,omitempty"', 'json:"app,omitempty"'),
    ('yaml:"apps,omitempty"', 'yaml:"apps,omitempty"'),
    ('yaml:"apps:"', 'yaml:"apps:"'),
    ("apps:", "apps:"),
    ("Apps:", "Apps:"),
    ("apps.", "apps."),
    ("Apps.", "Apps."),
    ("apps/", "apps/"),
    ("Apps/", "Apps/"),
    (" app ", " app "),
    (" apps ", " apps "),
    ("App ", "App "),
    ("Apps ", "Apps "),
    ("app.", "app."),
    ("apps.", "apps."),
]

FILE_RENAMES = [
    ("sdk/proto/v1/app.proto", "sdk/proto/v1/app.proto"),
    ("sdk/proto/v1/appruntime.proto", "sdk/proto/v1/appruntime.proto"),
    ("sdk/typescript/src/app.ts", "sdk/typescript/src/app.ts"),
    ("sdk/typescript/src/appruntime.ts", "sdk/typescript/src/appruntime.ts"),
    ("sdk/python/gestalt/_app.py", "sdk/python/gestalt/_app.py"),
    ("sdk/python/gestalt/_appruntime.py", "sdk/python/gestalt/_appruntime.py"),
    ("sdk/python/tests/test_app.py", "sdk/python/tests/test_app.py"),
    ("sdk/python/tests/test_app_invoker_transport.py", "sdk/python/tests/test_app_invoker_transport.py"),
    ("sdk/rust/src/app_runtime.rs", "sdk/rust/src/app_runtime.rs"),
    ("sdk/rust/tests/app_runtime_transport.rs", "sdk/rust/tests/app_runtime_transport.rs"),
    ("sdk/rust/tests/app_invoker_transport.rs", "sdk/rust/tests/app_invoker_transport.rs"),
    ("sdk/go/serve_appruntime.go", "sdk/go/serve_appruntime.go"),
    ("sdk/go/appruntime_provider.go", "sdk/go/appruntime_provider.go"),
    ("sdk/go/appruntime_transport_test.go", "sdk/go/appruntime_transport_test.go"),
    ("gestalt/cli/src/commands/apps.rs", "gestalt/cli/src/commands/apps.rs"),
    ("gestalt/cli/src/commands/app_errors.rs", "gestalt/cli/src/commands/app_errors.rs"),
    ("gestalt/cli/tests/apps_connect.rs", "gestalt/cli/tests/apps_connect.rs"),
    ("docs/content/providers/apps.mdx", "docs/content/providers/apps.mdx"),
    ("docs/content/reference/app-manifests.mdx", "docs/content/reference/app-manifests.mdx"),
    ("docs/content/solutions.mdx", "docs/content/solutions.mdx"),
    ("gestaltd/internal/config/app_validation.go", "gestaltd/internal/config/app_validation.go"),
    ("gestaltd/internal/config/app_scope.go", "gestaltd/internal/config/app_scope.go"),
    ("gestaltd/internal/bootstrap/app_runtime.go", "gestaltd/internal/bootstrap/app_runtime.go"),
    ("gestaltd/internal/bootstrap/app_runtime_plan.go", "gestaltd/internal/bootstrap/app_runtime_plan.go"),
    ("gestaltd/internal/bootstrap/app_runtime_launch.go", "gestaltd/internal/bootstrap/app_runtime_launch.go"),
    ("gestaltd/internal/bootstrap/app_runtime_inspect.go", "gestaltd/internal/bootstrap/app_runtime_inspect.go"),
    ("gestaltd/internal/bootstrap/app_runtime_executable.go", "gestaltd/internal/bootstrap/app_runtime_executable.go"),
    ("gestaltd/internal/bootstrap/app_runtime_executable_test.go", "gestaltd/internal/bootstrap/app_runtime_executable_test.go"),
    ("gestaltd/internal/bootstrap/executable_plugin_test.go", "gestaltd/internal/bootstrap/executable_app_test.go"),
    ("gestaltd/internal/testutil/sdkapp.go", "gestaltd/internal/testutil/sdkapp.go"),
    ("gestaltd/services/invocation/app_invocation_dependency.go", "gestaltd/services/invocation/app_invocation_dependency.go"),
    ("gestaltd/services/appinvoker/app_invoker_server.go", "gestaltd/services/appinvoker/app_invoker_server.go"),
    ("gestaltd/services/appinvoker/app_invoker_env.go", "gestaltd/services/appinvoker/app_invoker_env.go"),
    ("gestaltd/services/appinvoker/app_invoker_test.go", "gestaltd/services/appinvoker/app_invoker_test.go"),
]

DIR_RENAMES = [
    ("gestaltd/services/plugins", "gestaltd/services/apps"),
    ("gestaltd/services/appinvoker", "gestaltd/services/appinvoker"),
    ("gestaltd/services/runtimehost/appruntime", "gestaltd/services/runtimehost/appruntime"),
]


def should_skip(path: Path) -> bool:
    parts = set(path.parts)
    if parts & SKIP_DIRS:
        return True
    if path.suffix in SKIP_FILE_SUFFIXES and path.name not in {"bun.lock"}:
        if path.suffix in {".lock"}:
            pass
        elif path.suffix in SKIP_FILE_SUFFIXES:
            return True
    if path.name in BUF_GEN_YAML:
        return True
    if "vendor" in path.parts:
        return True
    return False


def apply_replacements(text: str, path: Path) -> str:
    if path.name in BUF_GEN_YAML:
        text = text.replace("v1/app.proto", "v1/app.proto")
        text = text.replace("v1/appruntime.proto", "v1/appruntime.proto")
        return text
    for old, new in REPLACEMENTS:
        text = text.replace(old, new)
    return text


def transform_proto_files() -> None:
    proto_dir = ROOT / "sdk/proto/v1"
    mappings = [
        ("app.proto", "app.proto"),
        ("appruntime.proto", "appruntime.proto"),
    ]
    for src_name, dst_name in mappings:
        src = proto_dir / src_name
        if not src.exists():
            continue
        content = apply_replacements(src.read_text(), src)
        content = content.replace("StartPlugin(", "StartApp(")
        content = content.replace("StartApp ", "StartApp ")
        (proto_dir / dst_name).write_text(content)
        src.unlink()


def walk_files() -> list[Path]:
    files: list[Path] = []
    for dirpath, dirnames, filenames in os.walk(ROOT):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for name in filenames:
            path = Path(dirpath) / name
            if should_skip(path):
                continue
            if path.suffix in {".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".woff", ".woff2"}:
                continue
            files.append(path)
    return files


def rewrite_contents() -> None:
    for path in walk_files():
        if path.suffix in {".pb.go", ".pb.ts", ".py", ".rs"} and "gen/" in str(path):
            continue
        try:
            raw = path.read_bytes()
        except OSError:
            continue
        try:
            text = raw.decode("utf-8")
        except UnicodeDecodeError:
            continue
        new_text = apply_replacements(text, path)
        if new_text != text:
            path.write_text(new_text)


def git_mv(src: Path, dst: Path) -> None:
    if not src.exists():
        return
    dst.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["git", "mv", str(src), str(dst)], cwd=ROOT, check=True)


def rename_dirs() -> None:
    for src_rel, dst_rel in DIR_RENAMES:
        git_mv(ROOT / src_rel, ROOT / dst_rel)


def rename_files() -> None:
    for src_rel, dst_rel in FILE_RENAMES:
        git_mv(ROOT / src_rel, ROOT / dst_rel)


def fix_workflow_proto_oneof() -> None:
    path = ROOT / "sdk/proto/v1/workflow.proto"
    if not path.exists():
        return
    text = path.read_text()
    text = text.replace("BoundWorkflowAppTarget app = 6;", "BoundWorkflowAppTarget app = 6;")
    text = text.replace('reserved "app_name"', 'reserved "app_name"')
    path.write_text(text)


def fix_runtime_proto() -> None:
    path = ROOT / "sdk/proto/v1/runtime.proto"
    if not path.exists():
        return
    text = path.read_text()
    text = text.replace("PROVIDER_KIND_APP = 1;", "PROVIDER_KIND_APP = 1;")
    path.write_text(text)


def fix_buf_rust_paths() -> None:
    path = ROOT / "sdk/proto/buf.rust.gen.yaml"
    if not path.exists():
        return
    text = path.read_text()
    text = text.replace("v1/app.proto", "v1/app.proto")
    text = text.replace("v1/appruntime.proto", "v1/appruntime.proto")
    path.write_text(text)


def fix_cli_mod() -> None:
    path = ROOT / "gestalt/cli/src/commands/mod.rs"
    if not path.exists():
        return
    text = path.read_text()
    text = text.replace("mod app_errors;", "mod app_errors;")
    text = text.replace("pub mod plugins;", "pub mod apps;")
    path.write_text(text)


def fix_cli_main() -> None:
    for rel in ["gestalt/cli/src/cli.rs", "gestalt/cli/src/lib.rs", "gestalt/cli/src/main.rs"]:
        path = ROOT / rel
        if not path.exists():
            continue
        text = path.read_text()
        text = text.replace("commands::plugins", "commands::apps")
        text = text.replace("AppCommands", "AppCommands")
        text = text.replace("App {", "App {")
        text = text.replace("Manage plugins", "Manage apps")
        text = text.replace("#[command(aliases = [\"plugins\", \"integrations\"])]", '#[command(alias = "apps")]')
        text = text.replace("plugin, workflow", "app, workflow")
        path.write_text(text)


def regenerate_proto() -> None:
    proto_root = ROOT / "sdk/proto"
    templates = [
        "buf.go.server.gen.yaml",
        "buf.go.sdk.gen.yaml",
        "buf.typescript.gen.yaml",
        "buf.python.gen.yaml",
        "buf.rust.gen.yaml",
    ]
    for tpl in templates:
        subprocess.run(["buf", "generate", "--template", tpl], cwd=proto_root, check=True)


def delete_old_generated() -> None:
    patterns = [
        "**/app_pb.*",
        "**/appruntime_pb.*",
        "**/app.pb.go",
        "**/plugin_grpc.pb.go",
        "**/appruntime.pb.go",
        "**/appruntime_grpc.pb.go",
        "**/app_pb2*",
        "**/appruntime_pb2*",
    ]
    for pattern in patterns:
        for path in ROOT.glob(pattern):
            if path.is_file() and "vendor" not in path.parts:
                path.unlink(missing_ok=True)


def main() -> int:
    os.chdir(ROOT)
    transform_proto_files()
    fix_workflow_proto_oneof()
    fix_runtime_proto()
    fix_buf_rust_paths()
    rename_dirs()
    rename_files()
    rewrite_contents()
    fix_cli_mod()
    fix_cli_main()
    delete_old_generated()
    try:
        regenerate_proto()
    except subprocess.CalledProcessError as exc:
        print(f"buf generate failed: {exc}", file=sys.stderr)
        return 1
    # Second pass after codegen
    rewrite_contents()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
