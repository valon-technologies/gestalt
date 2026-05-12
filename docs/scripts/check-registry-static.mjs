import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { existsSync, statSync } from "node:fs";
import path from "node:path";

const siteRoot = path.resolve("out");
const registryHtml = path.join(siteRoot, "registry.html");

if (!existsSync(registryHtml)) {
  throw new Error("out/registry.html is missing; run npm run build first");
}

const contentTypes = new Map([
  [".css", "text/css"],
  [".html", "text/html"],
  [".js", "text/javascript"],
  [".json", "application/json"],
  [".svg", "image/svg+xml"],
  [".txt", "text/plain"],
  [".woff", "font/woff"],
  [".woff2", "font/woff2"],
]);

const sdkReferenceTargets = {
  python: "/api/python/index.html",
  typescript: "/api/typescript/index.html",
  go: "https://pkg.go.dev/github.com/valon-technologies/gestalt/sdk/go",
  rust: "https://docs.rs/gestalt-sdk/latest/gestalt/",
};
const registryMarker = "registry_shell__";

const server = createServer(async (request, response) => {
  try {
    const host =
      request.headers["x-test-host"]?.split(":")[0] ??
      request.headers.host?.split(":")[0] ??
      "";
    const url = new URL(request.url ?? "/", "http://localhost");
    const pathname = decodeURIComponent(url.pathname);
    if (host === "registry.gestaltd.ai") {
      await serveRegistryHost(pathname, response);
    } else {
      await serveDocsHost(pathname, response);
    }
  } catch (error) {
    response.writeHead(500, { "content-type": "text/plain" });
    response.end(error instanceof Error ? error.message : "server error");
  }
});

await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));

try {
  const { port } = server.address();
  const baseUrl = `http://127.0.0.1:${port}`;
  await assertResponse({
    url: `${baseUrl}/`,
    host: "registry.gestaltd.ai",
    status: 200,
    includes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/providers`,
    host: "registry.gestaltd.ai",
    status: 200,
    includes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/`,
    host: "registry.gestaltd.ai",
    status: 200,
    includes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/events/`,
    host: "registry.gestaltd.ai",
    status: 200,
    includes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/_next/static/chunks/does-not-exist.js`,
    host: "registry.gestaltd.ai",
    status: 404,
    excludes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/providers`,
    host: "gestaltd.ai",
    status: 301,
    location: "/latest/providers",
  });
  await assertRedirect({
    url: `${baseUrl}/`,
    host: "gestaltd.ai",
    location: "/latest/",
  });
  await assertRedirect({
    url: `${baseUrl}/getting-started`,
    host: "gestaltd.ai",
    location: "/latest/getting-started",
  });
  await assertRedirect({
    url: `${baseUrl}/latest`,
    host: "gestaltd.ai",
    location: "/latest/",
  });
  await assertResponse({
    url: `${baseUrl}/latest/`,
    host: "gestaltd.ai",
    status: 200,
    includes: "Gestalt",
    excludes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/latest/getting-started`,
    host: "gestaltd.ai",
    status: 200,
    includes: "Getting Started",
    excludes: registryMarker,
  });
  await assertRedirect({
    url: `${baseUrl}/latest/getting-started/`,
    host: "gestaltd.ai",
    location: "/latest/getting-started",
  });
  await assertRedirect({
    url: `${baseUrl}/versions/v0.0.1-alpha.24`,
    host: "gestaltd.ai",
    location: "/versions/v0.0.1-alpha.24/",
  });
  await assertResponse({
    url: `${baseUrl}/versions/v0.0.1-alpha.24/`,
    host: "gestaltd.ai",
    status: 200,
    includes: "Gestalt",
    excludes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/versions/v0.0.1-alpha.24/reference/cli`,
    host: "gestaltd.ai",
    status: 200,
    includes: "Server CLI",
    excludes: registryMarker,
  });
  await assertRedirect({
    url: `${baseUrl}/versions/v0.0.1-alpha.24/reference/cli/`,
    host: "gestaltd.ai",
    location: "/versions/v0.0.1-alpha.24/reference/cli",
  });
  await assertResponse({
    url: `${baseUrl}/versions/v0.0.1-alpha.24/reference/sdk/python`,
    host: "gestaltd.ai",
    status: 200,
    includes: "Python SDK",
    excludes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/versions/v9.9.9/`,
    host: "gestaltd.ai",
    status: 404,
    excludes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/versions.json`,
    host: "gestaltd.ai",
    status: 200,
    includes: "\"versions\"",
  });
  await assertResponse({
    url: `${baseUrl}/api/does-not-exist`,
    host: "gestaltd.ai",
    status: 404,
    excludes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/install.sh`,
    host: "gestaltd.ai",
    status: 200,
    includes: "Install Gestalt",
  });
  await assertResponse({
    url: `${baseUrl}/registry/providers/plugin/slack/`,
    host: "gestaltd.ai",
    status: 200,
    includes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/registry/providers/plugin/slack/events/`,
    host: "gestaltd.ai",
    status: 200,
    includes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/`,
    host: "gestaltd.ai",
    status: 404,
    excludes: registryMarker,
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/events/`,
    host: "gestaltd.ai",
    status: 404,
    excludes: registryMarker,
  });
  for (const [language, location] of Object.entries(sdkReferenceTargets)) {
    for (const prefix of [`/reference/${language}-sdk`, `/reference/sdk/${language}`]) {
      for (const suffix of ["", ".html", "/"]) {
        await assertRedirect({
          url: `${baseUrl}${prefix}${suffix}`,
          host: "gestaltd.ai",
          location,
        });
      }
    }
  }
} finally {
  server.close();
}

async function serveRegistryHost(pathname, response) {
  if (
    pathname.startsWith("/_next/") ||
    pathname.startsWith("/_pagefind/") ||
    pathname === "/favicon.svg" ||
    pathname.startsWith("/images/") ||
    pathname.startsWith("/fonts/") ||
    pathname === "/404.html"
  ) {
    return serveFile(pathname, response, false);
  }
  return serveFile("/registry.html", response, false);
}

async function serveDocsHost(pathname, response) {
  if (pathname === "/") {
    return redirect(response, "/latest/");
  }
  if (pathname === "/latest") {
    return redirect(response, "/latest/");
  }
  const versionWithoutSlash = pathname.match(/^\/versions\/v[^/]+$/);
  if (versionWithoutSlash) {
    return redirect(response, `${pathname}/`);
  }
  const latestPageWithSlash = pathname.match(/^(\/latest\/.+)\/$/);
  if (latestPageWithSlash) {
    return redirect(response, latestPageWithSlash[1]);
  }
  const versionPageWithSlash = pathname.match(/^(\/versions\/v[^/]+\/.+)\/$/);
  if (versionPageWithSlash) {
    return redirect(response, versionPageWithSlash[1]);
  }
  if (pathname.startsWith("/latest/") || pathname.startsWith("/versions/")) {
    return serveFile(pathname, response, true, null);
  }
  if (pathname === "/registry" || pathname.startsWith("/registry/")) {
    return serveFile("/registry.html", response, false);
  }
  if (
    pathname.startsWith("/api/") ||
    pathname.startsWith("/_next/") ||
    pathname.startsWith("/_pagefind/") ||
    pathname === "/favicon.svg" ||
    pathname.startsWith("/images/") ||
    pathname.startsWith("/fonts/") ||
    pathname === "/versions.json" ||
    pathname === "/404.html" ||
    pathname === "/install.sh" ||
    pathname === "/install-gestaltd.sh" ||
    pathname === "/install-gestalt.sh"
  ) {
    return serveFile(pathname, response, true, null);
  }
  const sdkReferenceTarget = getSdkReferenceTarget(pathname);
  if (sdkReferenceTarget) {
    return redirect(response, sdkReferenceTarget);
  }
  if (/^\/providers\/[^/]+\/.+/.test(pathname)) {
    response.writeHead(404, { "content-type": "text/plain" });
    response.end("not found");
    return;
  }
  return redirect(response, `/latest${pathname}`);
}

function getSdkReferenceTarget(pathname) {
  const normalized = pathname.replace(/\/$/, "").replace(/\.html$/, "");
  const match = normalized.match(
    /^\/reference\/(?:(python|typescript|go|rust)-sdk|sdk\/(python|typescript|go|rust))$/,
  );
  const language = match?.[1] ?? match?.[2];
  return sdkReferenceTargets[language] ?? null;
}

async function serveFile(pathname, response, htmlFallback, fallbackFile = null) {
  const candidates = candidateFiles(pathname);
  for (const candidate of candidates) {
    if (
      candidate.startsWith(siteRoot) &&
      existsSync(candidate) &&
      statSync(candidate).isFile()
    ) {
      const ext = path.extname(candidate);
      response.writeHead(200, {
        "content-type": contentTypes.get(ext) ?? "application/octet-stream",
      });
      response.end(await readFile(candidate));
      return;
    }
  }
  if (htmlFallback && fallbackFile) {
    response.writeHead(200, { "content-type": "text/html" });
    response.end(await readFile(fallbackFile));
    return;
  }
  response.writeHead(404, { "content-type": "text/plain" });
  response.end("not found");
}

function candidateFiles(pathname) {
  const cleanPath = pathname === "/" ? "/index" : pathname.replace(/\/$/, "");
  const fullPath = path.join(siteRoot, cleanPath);
  return [
    fullPath,
    `${fullPath}.html`,
    path.join(siteRoot, pathname, "index.html"),
  ];
}

async function assertResponse({ url, host, status, includes, excludes, location }) {
  const response = await fetch(url, {
    headers: { "x-test-host": host },
    redirect: "manual",
  });
  const body = await response.text();
  if (response.status !== status) {
    throw new Error(`${host} ${url} returned ${response.status}, want ${status}`);
  }
  if (status >= 300 && status < 400 && location) {
    const actualLocation = response.headers.get("location");
    if (actualLocation !== location) {
      throw new Error(`${host} ${url} redirected to ${actualLocation}, want ${location}`);
    }
  }
  if (includes && !body.includes(includes)) {
    throw new Error(`${host} ${url} did not include ${includes}`);
  }
  if (excludes && body.includes(excludes)) {
    throw new Error(`${host} ${url} unexpectedly included ${excludes}`);
  }
}

function redirect(response, location) {
  response.writeHead(301, { location });
  response.end();
}

async function assertRedirect({ url, host, location }) {
  const response = await fetch(url, {
    headers: { "x-test-host": host },
    redirect: "manual",
  });
  if (response.status !== 301) {
    throw new Error(`${host} ${url} returned ${response.status}, want 301`);
  }
  const actualLocation = response.headers.get("location");
  if (actualLocation !== location) {
    throw new Error(`${host} ${url} redirected to ${actualLocation}, want ${location}`);
  }
}
