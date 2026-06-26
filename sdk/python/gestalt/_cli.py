from __future__ import annotations

import sys

from gestalt._build import main as build_main
from gestalt._runtime import main as run_main

USAGE = "usage: gestalt <build|run> [args...]"


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    if not args:
        print(USAGE, file=sys.stderr)
        return 2
    subcommand, *rest = args
    if subcommand == "build":
        return build_main(rest)
    if subcommand == "run":
        return run_main(rest)
    print(USAGE, file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
