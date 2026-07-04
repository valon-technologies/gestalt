/**
 * Normalize a mount path to a leading and trailing slash, or "/" when empty.
 *
 * @param {string | undefined | null} value
 * @returns {string}
 */
export function normalizeBasePath(value) {
  if (value == null || String(value).trim() === "") {
    return "/";
  }
  let path = String(value).trim();
  if (!path.startsWith("/")) {
    path = `/${path}`;
  }
  if (!path.endsWith("/")) {
    path = `${path}/`;
  }
  return path;
}

/**
 * @param {string | undefined} value
 * @returns {number}
 */
function parseDevPort(value) {
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`Invalid GESTALT_DEV_PORT: ${value ?? ""}`);
  }
  return port;
}

/**
 * @param {number} port
 * @returns {import('vite').ServerOptions}
 */
function devBinding(port) {
  return {
    host: "127.0.0.1",
    port,
    strictPort: true,
    allowedHosts: true,
  };
}

/**
 * @param {Record<string, string | undefined>} env
 * @param {string} command
 * @returns {import('vite').UserConfig | null}
 */
function gestaltConfig(env, command) {
  if (command === "build") {
    const outDir = env.GESTALT_BUILD_STATIC?.trim();
    if (!outDir) {
      return null;
    }
    return {
      base: "./",
      build: {
        outDir,
        emptyOutDir: true,
      },
    };
  }

  const devPort = env.GESTALT_DEV_PORT?.trim();
  if (!devPort) {
    return null;
  }

  const port = parseDevPort(devPort);
  const base = normalizeBasePath(env.GESTALT_DEV_BASE_PATH);
  const binding = devBinding(port);

  return {
    base,
    server: binding,
    preview: binding,
  };
}

/**
 * @param {{ env?: Record<string, string | undefined> }} [options]
 * @returns {import('vite').Plugin}
 */
export function gestalt(options = {}) {
  const env = options.env ?? process.env;

  return {
    name: "gestalt",
    enforce: "post",
    config(_userConfig, { command }) {
      return gestaltConfig(env, command);
    },
    transformIndexHtml: {
      order: "pre",
      handler(html) {
        if (!env.GESTALT_DEV_PORT?.trim()) {
          return html;
        }
        const devBase = normalizeBasePath(env.GESTALT_DEV_BASE_PATH);
        // Root mount: relative refs resolve without <base>; gestaltd skips it in prod too.
        if (devBase === "/") {
          return html;
        }
        if (/<base\b/i.test(html)) {
          return html;
        }
        return html.replace(
          /<head(\s[^>]*)?>/i,
          (match) => `${match}<base href="${devBase}">`,
        );
      },
    },
  };
}
