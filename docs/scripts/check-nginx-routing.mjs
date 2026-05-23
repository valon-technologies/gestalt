import { spawnSync } from "node:child_process";
import { readdirSync, statSync } from "node:fs";
import { request } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const image = `gestalt-docs-nginx-routing:${process.pid}`;
const docsRoot = fileURLToPath(new URL("..", import.meta.url));
let container = "";

try {
  run("docker", ["build", "-t", image, "-f", "Dockerfile", "."]);
  container = output("docker", [
    "run",
    "-d",
    "--rm",
    "-p",
    "127.0.0.1::8080",
    image,
  ]);
  const port = output("docker", ["port", container, "8080/tcp"]).match(
    /127\.0\.0\.1:(\d+)/,
  )?.[1];
  if (!port) {
    throw new Error(`could not determine mapped port for ${container}`);
  }
  const baseUrl = `http://127.0.0.1:${port}`;
  const devAssetPath = findStaticAssetPath("dev/_next/static");
  const latestAssetPath = findStaticAssetPath("latest/_next/static");
  await waitForNginx(baseUrl);
  const versionsManifest = await readVersionsManifest(
    `${baseUrl}/versions.json`,
    "gestaltd.ai",
  );
  const exactVersionPrefix = versionsManifest.versions[0]?.pathPrefix;
  if (!exactVersionPrefix) {
    throw new Error("/versions.json did not contain any exact versions");
  }

  await assertResponse({
    url: `${baseUrl}/`,
    host: "gestaltd.ai",
    status: 301,
    location: "/latest/",
  });
  await assertResponse({
    url: `${baseUrl}/getting-started`,
    host: "gestaltd.ai",
    status: 301,
    location: "/latest/getting-started",
  });
  await readVersionsManifest(`${baseUrl}/versions.json`, "gestaltd.ai");
  await assertResponse({
    url: `${baseUrl}/dev`,
    host: "gestaltd.ai",
    status: 301,
    location: "/dev/",
  });
  await assertResponse({
    url: `${baseUrl}/dev/`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "no-cache",
    includes: "Gestalt",
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/dev/providers`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "no-cache",
    includes: "Providers",
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/dev/providers.txt`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "no-cache",
    includes: "What Providers Do",
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}${devAssetPath}`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "immutable",
  });
  await assertResponse({
    url: `${baseUrl}/dev/providers/`,
    host: "gestaltd.ai",
    status: 301,
    location: "/dev/providers",
  });
  await assertResponse({
    url: `${baseUrl}/dev/does-not-exist`,
    host: "gestaltd.ai",
    status: 404,
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/latest/`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "no-cache",
    includes: "Gestalt",
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/latest/providers`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "no-cache",
    includes: "Providers",
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/latest/providers.txt`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "no-cache",
    includes: "What Providers Do",
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}${latestAssetPath}`,
    host: "gestaltd.ai",
    status: 200,
    cacheControlIncludes: "immutable",
  });
  await assertResponse({
    url: `${baseUrl}/latest/getting-started/`,
    host: "gestaltd.ai",
    status: 301,
    location: "/latest/getting-started",
  });
  await assertResponse({
    url: `${baseUrl}${exactVersionPrefix}/reference/cli`,
    host: "gestaltd.ai",
    status: 200,
    includes: "Server CLI",
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}${exactVersionPrefix}/reference/cli/`,
    host: "gestaltd.ai",
    status: 301,
    location: `${exactVersionPrefix}/reference/cli`,
  });
  await assertResponse({
    url: `${baseUrl}/latest/api/python/index.html`,
    host: "gestaltd.ai",
    status: 301,
    location: "/api/python/index.html",
  });
  await assertResponse({
    url: `${baseUrl}${exactVersionPrefix}/api/typescript/index.html`,
    host: "gestaltd.ai",
    status: 301,
    location: "/api/typescript/index.html",
  });
  await assertResponse({
    url: `${baseUrl}/versions/v9.9.9/`,
    host: "gestaltd.ai",
    status: 404,
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/api/does-not-exist`,
    host: "gestaltd.ai",
    status: 404,
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/providers/app/slack/`,
    host: "gestaltd.ai",
    status: 404,
    excludes: "registry_shell__",
  });
  await assertResponse({
    url: `${baseUrl}/reference/sdk/python`,
    host: "gestaltd.ai",
    status: 301,
    location: "/api/python/index.html",
  });
  await assertResponse({
    url: `${baseUrl}/providers/app/slack/`,
    host: "registry.gestaltd.ai",
    status: 200,
    cacheControlIncludes: "no-cache",
    includes: "registry_shell__",
  });
} finally {
  if (container) {
    spawnSync("docker", ["stop", container], { stdio: "ignore" });
  }
  spawnSync("docker", ["image", "rm", image], { stdio: "ignore" });
}

async function waitForNginx(baseUrl) {
  const deadline = Date.now() + 10_000;
  let lastError = null;
  while (Date.now() < deadline) {
    try {
      const response = await httpGet(baseUrl, "gestaltd.ai");
      if (response.status === 301) {
        return;
      }
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  throw new Error(
    `nginx container did not become ready${lastError ? `: ${lastError}` : ""}`,
  );
}

async function assertResponse({
  url,
  host,
  status,
  includes,
  excludes,
  location,
  cacheControlIncludes,
}) {
  const response = await httpGet(url, host);
  const body = response.body;
  if (response.status !== status) {
    throw new Error(`${host} ${url} returned ${response.status}, want ${status}`);
  }
  if (location) {
    const actualLocation = response.headers.location;
    if (actualLocation !== location) {
      throw new Error(`${host} ${url} redirected to ${actualLocation}, want ${location}`);
    }
  }
  if (cacheControlIncludes) {
    const cacheControl = response.headers["cache-control"] ?? "";
    if (!cacheControl.includes(cacheControlIncludes)) {
      throw new Error(
        `${host} ${url} returned Cache-Control ${cacheControl}, want ${cacheControlIncludes}`,
      );
    }
  }
  if (includes && !body.includes(includes)) {
    throw new Error(`${host} ${url} did not include ${includes}`);
  }
  if (excludes && body.includes(excludes)) {
    throw new Error(`${host} ${url} unexpectedly included ${excludes}`);
  }
}

async function readVersionsManifest(url, host) {
  const response = await httpGet(url, host);
  if (response.status !== 200) {
    throw new Error(`${host} ${url} returned ${response.status}, want 200`);
  }
  const manifest = JSON.parse(response.body);
  if (!manifest.latest?.pathPrefix || !Array.isArray(manifest.versions)) {
    throw new Error("/versions.json did not contain a versions manifest");
  }
  if (
    manifest.development?.pathPrefix !== "/dev" ||
    manifest.development?.version !== "main" ||
    manifest.development?.development !== true
  ) {
    throw new Error("/versions.json did not contain the /dev development channel");
  }
  return manifest;
}

function httpGet(url, host) {
  const target = new URL(url);
  return new Promise((resolve, reject) => {
    const req = request(
      {
        headers: { Host: host },
        hostname: target.hostname,
        method: "GET",
        path: `${target.pathname}${target.search}`,
        port: target.port,
      },
      (res) => {
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () => {
          resolve({
            body: Buffer.concat(chunks).toString("utf8"),
            headers: res.headers,
            status: res.statusCode,
          });
        });
      },
    );
    req.on("error", reject);
    req.end();
  });
}

function run(command, args) {
  const result = spawnSync(command, args, {
    cwd: new URL("..", import.meta.url),
    encoding: "utf8",
    stdio: "inherit",
  });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed`);
  }
}

function output(command, args) {
  const result = spawnSync(command, args, {
    cwd: new URL("..", import.meta.url),
    encoding: "utf8",
  });
  if (result.status !== 0) {
    throw new Error(result.stderr || `${command} ${args.join(" ")} failed`);
  }
  return result.stdout.trim();
}

function findStaticAssetPath(relativeRoot) {
  const root = path.join(docsRoot, "out", relativeRoot);
  const stack = [root];
  while (stack.length > 0) {
    const current = stack.pop();
    for (const entry of readdirSync(current)) {
      const fullPath = path.join(current, entry);
      const stat = statSync(fullPath);
      if (stat.isDirectory()) {
        stack.push(fullPath);
        continue;
      }
      if (/\.(?:css|js)$/.test(entry)) {
        const relative = path
          .relative(path.join(docsRoot, "out"), fullPath)
          .split(path.sep)
          .join("/");
        return `/${relative}`;
      }
    }
  }
  throw new Error(`could not find a CSS or JS asset under out/${relativeRoot}`);
}
