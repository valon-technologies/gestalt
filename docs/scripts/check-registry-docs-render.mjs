import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { existsSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { createElement, Fragment } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ts from "typescript";

const siteRoot = path.resolve("out");
const fixtureRoot = path.resolve("scripts/fixtures/registry-docs");
const registryHtml = path.join(siteRoot, "registry.html");

if (!existsSync(registryHtml)) {
  throw new Error("out/registry.html is missing; run npm run build first");
}

const contentTypes = new Map([
  [".css", "text/css"],
  [".html", "text/html"],
  [".js", "text/javascript"],
  [".json", "application/json"],
  [".mdx", "text/markdown"],
  [".svg", "image/svg+xml"],
  [".woff", "font/woff"],
  [".woff2", "font/woff2"],
]);

const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url ?? "/", "http://localhost");
    const pathname = decodeURIComponent(url.pathname);
    if (pathname.startsWith("/registry-test/")) {
      return serveFixture(pathname, response);
    }
    if (
      pathname.startsWith("/_next/") ||
      pathname.startsWith("/_pagefind/") ||
      pathname === "/favicon.svg" ||
      pathname.startsWith("/images/") ||
      pathname.startsWith("/fonts/")
    ) {
      return serveFile(siteRoot, pathname, response);
    }
    return serveFile(siteRoot, "/registry.html", response);
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
    url: `${baseUrl}/providers/plugin/slack/`,
    status: 200,
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/events/`,
    status: 200,
    includes: "registry-module",
  });
  await assertResponse({
    url: `${baseUrl}/providers/plugin/slack/missing/`,
    status: 200,
    includes: "registry-module",
  });

  const catalogResponse = await fetch(`${baseUrl}/registry-test/catalog.json`);
  const catalog = await catalogResponse.json();
  const slack = catalog.providers.find(
    (provider) => provider.packagePath === "plugins/slack",
  );
  if (!slack) {
    throw new Error("fixture catalog missing plugins/slack");
  }
  if (slack.docs?.[0]?.path !== "/" || slack.docs?.[1]?.path !== "/events/") {
    throw new Error("fixture catalog docs paths are not provider-relative");
  }

  const overviewSource = await assertResponse({
    url: `${baseUrl}${slack.docs[0].rawUrl}`,
    status: 200,
    includes: "Fixture overview rendered from provider-owned MDX",
  });
  const eventsSource = await assertResponse({
    url: `${baseUrl}${slack.docs[1].rawUrl}`,
    status: 200,
    includes: "Fixture events rendered from provider-owned MDX",
  });
  await assertRenderedDocs({
    eventsSource,
    overviewSource,
  });
} finally {
  server.close();
}

async function serveFixture(pathname, response) {
  const fixturePath = pathname.replace(/^\/registry-test\//, "");
  return serveFile(fixtureRoot, `/${fixturePath}`, response);
}

async function serveFile(root, pathname, response) {
  const fullPath = path.join(root, pathname);
  if (
    !fullPath.startsWith(root) ||
    !existsSync(fullPath) ||
    !statSync(fullPath).isFile()
  ) {
    response.writeHead(404, { "content-type": "text/plain" });
    response.end("not found");
    return;
  }
  const ext = path.extname(fullPath);
  response.writeHead(200, {
    "access-control-allow-origin": "*",
    "content-type": contentTypes.get(ext) ?? "application/octet-stream",
  });
  response.end(await readFile(fullPath));
}

async function assertResponse({ url, status, includes }) {
  const response = await fetch(url);
  const body = await response.text();
  if (response.status !== status) {
    throw new Error(`${url} returned ${response.status}, want ${status}`);
  }
  if (includes && !body.includes(includes)) {
    throw new Error(`${url} did not include ${includes}`);
  }
  return body;
}

async function assertRenderedDocs({ overviewSource, eventsSource }) {
  const renderMarkdown = await loadRegistryMarkdownRenderer();
  const overviewHtml = renderFixtureMarkdown(renderMarkdown, overviewSource);
  if (!overviewHtml.includes("<h2>Slack</h2>")) {
    throw new Error("renderer did not render the fixture overview heading");
  }
  if (!overviewHtml.includes('href="/providers/plugin/slack/events/"')) {
    throw new Error("renderer did not resolve provider-relative MDX links");
  }
  const eventsHtml = renderFixtureMarkdown(renderMarkdown, eventsSource);
  if (!eventsHtml.includes("Fixture events rendered from provider-owned MDX")) {
    throw new Error("renderer did not render the fixture events source");
  }

  const continuationHtml = renderFixtureMarkdown(
    renderMarkdown,
    [
      "# README fallback",
      "",
      "- optional Codex surfaces such as apps, multi-agent tools, hooks, skills, and",
      "  web search disabled in the generated per-turn config",
      "- `connection`: Optional database/sql pool and retry tuning.",
      "  - `max_open_conns`: Maximum open connections.",
    ].join("\n"),
  );
  const listItemCount = continuationHtml.match(/<li>/g)?.length ?? 0;
  if (listItemCount !== 2) {
    throw new Error(`renderer split README continuation lists into ${listItemCount} items`);
  }
  if (!continuationHtml.includes("web search disabled in the generated per-turn config")) {
    throw new Error("renderer dropped README list continuation text");
  }
}

function renderFixtureMarkdown(renderMarkdown, source) {
  const nodes = renderMarkdown(source, {
    resolveLink: (href) => {
      if (href === "events.mdx") {
        return { href: "/providers/plugin/slack/events/", external: false };
      }
      if (href.startsWith("http")) {
        return { href, external: true };
      }
      return null;
    },
  });
  return renderToStaticMarkup(createElement(Fragment, null, ...nodes));
}

async function loadRegistryMarkdownRenderer() {
  const sourcePath = path.resolve("app/registry/registry-markdown.tsx");
  const source = await readFile(sourcePath, "utf8");
  const transpiled = ts.transpileModule(source, {
    compilerOptions: {
      esModuleInterop: true,
      jsx: ts.JsxEmit.ReactJSX,
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
    fileName: sourcePath,
  }).outputText;
  const require = createRequire(import.meta.url);
  const testModule = { exports: {} };
  const compile = new Function(
    "exports",
    "require",
    "module",
    "__filename",
    "__dirname",
    transpiled,
  );
  compile(
    testModule.exports,
    require,
    testModule,
    sourcePath,
    path.dirname(sourcePath),
  );
  if (typeof testModule.exports.renderMarkdown !== "function") {
    throw new Error("registry markdown renderer did not export renderMarkdown");
  }
  return testModule.exports.renderMarkdown;
}
