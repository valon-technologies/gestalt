from __future__ import annotations

import types
from typing import Any, Union, get_args, get_origin


def strip_optional(annotation: Any) -> Any:
    origin = get_origin(annotation)
    if origin not in (Union, types.UnionType):
        return annotation

    args = [arg for arg in get_args(annotation) if arg is not type(None)]
    if len(args) == 1:
        return args[0]
    return annotation


def is_optional_type(annotation: Any) -> bool:
    origin = get_origin(annotation)
    return origin in (Union, types.UnionType) and type(None) in get_args(annotation)
