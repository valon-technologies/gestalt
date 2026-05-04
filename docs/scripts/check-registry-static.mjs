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
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers`,
    host: "registry.gestaltd.ai",
    status: 200,
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/`,
    host: "registry.gestaltd.ai",
    status: 200,
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/events/`,
    host: "registry.gestaltd.ai",
    status: 200,
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/_next/static/chunks/does-not-exist.js`,
    host: "registry.gestaltd.ai",
    status: 404,
    excludes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers`,
    host: "gestaltd.ai",
    status: 200,
    excludes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/registry/providers/plugin/slack/`,
    host: "gestaltd.ai",
    status: 200,
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/registry/providers/plugin/slack/events/`,
    host: "gestaltd.ai",
    status: 200,
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/`,
    host: "gestaltd.ai",
    status: 404,
    excludes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/events/`,
    host: "gestaltd.ai",
    status: 404,
    excludes: "registry-module",
  });
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
  if (pathname === "/registry" || pathname.startsWith("/registry/")) {
    return serveFile("/registry.html", response, false);
  }
  if (pathname.match(/^\/reference\/(python|typescript|go|rust)-sdk(?:\.html)?$/)) {
    const target = pathname.replace(/-sdk(?:\.html)?$/, "");
    response.writeHead(301, { location: `/reference/sdk${target.replace("/reference", "")}` });
    response.end();
    return;
  }
  return serveFile(pathname, response, true, null);
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

async function assertResponse({ url, host, status, includes, excludes }) {
  const response = await fetch(url, { headers: { "x-test-host": host } });
  const body = await response.text();
  if (response.status !== status) {
    throw new Error(`${host} ${url} returned ${response.status}, want ${status}`);
  }
  if (includes && !body.includes(includes)) {
    throw new Error(`${host} ${url} did not include ${includes}`);
  }
  if (excludes && body.includes(excludes)) {
    throw new Error(`${host} ${url} unexpectedly included ${excludes}`);
  }
}
