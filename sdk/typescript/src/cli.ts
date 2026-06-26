#!/usr/bin/env bun

import { main as buildMain } from "./build.ts";
import { main as runMain } from "./providers/runtime.ts";

const USAGE = "usage: gestalt <build|run> [args...]";

export async function main(argv: string[] = process.argv.slice(2)): Promise<number> {
  const [subcommand, ...rest] = argv;
  switch (subcommand) {
    case "build":
      return buildMain(rest);
    case "run":
      return runMain(rest);
    default:
      console.error(USAGE);
      return 2;
  }
}

if (import.meta.main) {
  const code = await main();
  process.exit(code);
}
