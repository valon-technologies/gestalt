import { createReadStream, existsSync, statSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";
import { extname, join, normalize, sep } from "node:path";
import { fileURLToPath } from "node:url";

const MIME: Record<string, string> = {
  ".woff2": "font/woff2",
  ".woff": "font/woff",
  ".ttf": "font/ttf",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".png": "image/png",
};

/** Canonical shell brand fonts (gestaltd theme.assetsDir → `/theme/fonts` in prod). */
export const SHELL_FONTS_DIR = fileURLToPath(
  new URL("../../deploy/ui/fonts", import.meta.url),
);

// Minimal structural view of the Vite dev-server surface this plugin touches.
// Typing it locally keeps the shared registry script free of a `vite` import,
// which does not resolve when an app builds it across package boundaries.
type ThemeMiddleware = (
  req: IncomingMessage,
  res: ServerResponse,
  next: (err?: unknown) => void,
) => void;

interface DevServerLike {
  middlewares: { use(fn: ThemeMiddleware): unknown };
}

interface ShellThemeVitePlugin {
  name: string;
  apply: "serve";
  configureServer(server: DevServerLike): void;
}

export interface ServeShellThemeInDevOptions {
  /**
   * App `public/` directory. Optional overlay for non-font `/theme/*` assets.
   * Brand fonts always resolve from {@link SHELL_FONTS_DIR} — no sync-fonts copy.
   */
  publicDir?: string;
  /** Override fonts root (tests). Defaults to deploy/ui/fonts. */
  fontsDir?: string;
}

/**
 * Resolve a `/theme/...` request to a filesystem path under the shell contract.
 *
 * `/theme/fonts/*` → canonical deploy fonts (platform-owned).
 * Other `/theme/*` → optional `public/theme/*` overlay.
 */
export function resolveShellThemePath(
  reqPath: string,
  options: { publicDir?: string; fontsDir?: string } = {},
): string | null {
  if (!reqPath.startsWith("/theme/")) return null;

  const fontsDir = options.fontsDir ?? SHELL_FONTS_DIR;
  const relative = reqPath.replace(/^\/+/, ""); // theme/...

  if (relative === "theme/fonts" || relative.startsWith("theme/fonts/")) {
    const underFonts = relative.slice("theme/fonts".length).replace(/^\/+/, "");
    const file = normalize(join(fontsDir, underFonts));
    if (!file.startsWith(fontsDir + sep) && file !== fontsDir) return null;
    return file;
  }

  if (!options.publicDir) return null;
  const publicDir = options.publicDir;
  const file = normalize(join(publicDir, relative));
  if (!file.startsWith(publicDir + sep) && file !== publicDir) return null;
  return file;
}

/**
 * Dev-only Vite plugin: serve shell theme URLs at absolute `/theme/*`.
 *
 * Brand `@font-face` rules point at `/theme/fonts/*`, which the gestalt shell
 * serves in production. A standalone `vite` server has no shell — this plugin
 * serves those fonts from {@link SHELL_FONTS_DIR} so apps do not copy fonts into
 * `public/theme/fonts`.
 *
 * @param publicDirOrOptions App `public/` path (legacy) or options object.
 */
export function serveShellThemeInDev(
  publicDirOrOptions?: string | ServeShellThemeInDevOptions,
): ShellThemeVitePlugin {
  const options: ServeShellThemeInDevOptions =
    typeof publicDirOrOptions === "string" || publicDirOrOptions === undefined
      ? { publicDir: publicDirOrOptions }
      : publicDirOrOptions;

  const fontsDir = options.fontsDir ?? SHELL_FONTS_DIR;
  const publicDir = options.publicDir;

  return {
    name: "serve-shell-theme-in-dev",
    apply: "serve",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const reqPath = (req.url ?? "").split("?")[0] ?? "";
        const file = resolveShellThemePath(reqPath, { publicDir, fontsDir });
        if (!file || !existsSync(file) || !statSync(file).isFile()) {
          return next();
        }
        const mime = MIME[extname(file)];
        if (mime) res.setHeader("Content-Type", mime);
        res.setHeader("Cache-Control", "no-cache");
        createReadStream(file).pipe(res);
      });
    },
  };
}
