import { copyFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const uiDir = dirname(scriptDir);
const source = join(uiDir, "shared", "theme.css");
const target = join(uiDir, "..", "internal", "adminui", "out", "theme.css");

mkdirSync(dirname(target), { recursive: true });
copyFileSync(source, target);
