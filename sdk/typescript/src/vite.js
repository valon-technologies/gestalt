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

const DEFAULT_DEV_API_PROXY_TARGET = "http://127.0.0.1:8080";
const DEV_API_PROXY_TOKEN_ENV = "GESTALT_DEV_API_PROXY_TOKEN";

/**
 * @param {string | undefined | null} value
 * @returns {string}
 */
function normalizeApiProxyTarget(value) {
  const trimmed = value?.trim();
  if (!trimmed) {
    return DEFAULT_DEV_API_PROXY_TARGET;
  }
  return trimmed.replace(/\/+$/, "");
}

/**
 * @param {Record<string, string | undefined>} env
 * @returns {string}
 */
function resolveGestaltDevApiProxyTarget(env) {
  return normalizeApiProxyTarget(
    env.GESTALT_API_PROXY_TARGET?.trim() || env.GESTALT_BASE_URL?.trim(),
  );
}

/**
 * Keep the bearer capability behind the loopback dev-server boundary. Mounted
 * UIs can still be served through gestaltd with a foreign Host header, but
 * those requests must not activate this credential injection path.
 *
 * @param {import('http').IncomingMessage} req
 * @param {string} devPort
 * @param {string} apiTarget
 * @returns {boolean}
 */
function canInjectDevApiToken(req, devPort, apiTarget) {
  let target;
  try {
    target = new URL(apiTarget);
  } catch {
    return false;
  }
  if (!["127.0.0.1", "localhost", "[::1]"].includes(target.hostname)) {
    return false;
  }

  const host = req.headers.host?.toLowerCase();
  const allowedHosts = new Set([
    `127.0.0.1:${devPort}`,
    `localhost:${devPort}`,
    `[::1]:${devPort}`,
  ]);
  if (!host || !allowedHosts.has(host)) {
    return false;
  }

  const origin = req.headers.origin?.toLowerCase();
  if (origin && !allowedHosts.has(origin.replace(/^https?:\/\//, ""))) {
    return false;
  }
  if (req.headers["sec-fetch-site"] === "cross-site") {
    return false;
  }
  return true;
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
  const apiTarget = resolveGestaltDevApiProxyTarget(env);
  const token = env[DEV_API_PROXY_TOKEN_ENV]?.trim();
  const proxy = {
    target: apiTarget,
    changeOrigin: true,
    configure(proxyServer) {
      proxyServer.on("proxyReq", (proxyReq, req) => {
        if (
          token &&
          !req.headers.authorization &&
          canInjectDevApiToken(req, devPort, apiTarget)
        ) {
          proxyReq.setHeader("Authorization", `Bearer ${token}`);
        }
      });
    },
  };

  return {
    base,
    server: {
      ...binding,
      proxy: {
        "/api/v1": proxy,
      },
    },
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
