import { copyFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const uiDir = dirname(scriptDir);
const adminOut = join(uiDir, "..", "internal", "adminui", "out");

const themeSource = join(uiDir, "shared", "theme.css");
const themeTarget = join(adminOut, "theme.css");
mkdirSync(dirname(themeTarget), { recursive: true });
copyFileSync(themeSource, themeTarget);

const fontFiles = [
  "SeasonSerif_Regular.woff",
  "KMRMelangeGrotesk_Regular.woff",
  "KMRMelangeGrotesk_Bold.woff",
  "GeistMono_Regular.woff2",
];
const fontsSource = join(uiDir, "public", "fonts");
const fontsTarget = join(adminOut, "fonts");
mkdirSync(fontsTarget, { recursive: true });
for (const file of fontFiles) {
  copyFileSync(join(fontsSource, file), join(fontsTarget, file));
}
