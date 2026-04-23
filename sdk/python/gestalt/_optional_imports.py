from __future__ import annotations


def is_optional_provider_import_error(exc: ModuleNotFoundError) -> bool:
    missing_module = exc.name or ""
    return missing_module in {"google", "grpc"} or missing_module.startswith(
        ("google.", "grpc.")
    )
