import { EventEmitter } from "node:events";
import { createServer as createNetServer } from "node:net";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";
import { build, createServer as createViteServer, resolveConfig } from "vite";

import { gestalt, normalizeBasePath } from "../src/vite.js";
import { fixturePath, makeTempDir, removeTempDir } from "./helpers.ts";

const fixtureRoot = fixturePath("vite-app");

function listen(port: number): Promise<ReturnType<typeof createNetServer>> {
  return new Promise((resolve, reject) => {
    const server = createNetServer();
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => resolve(server));
  });
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createNetServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (address == null || typeof address === "string") {
        server.close(() => reject(new Error("failed to allocate port")));
        return;
      }
      const { port } = address;
      server.close((err) => (err ? reject(err) : resolve(port)));
    });
  });
}

describe("normalizeBasePath", () => {
  const cases: Array<[string | undefined | null, string]> = [
    [undefined, "/"],
    [null, "/"],
    ["", "/"],
    ["   ", "/"],
    ["/", "/"],
    ["example", "/example/"],
    ["/example", "/example/"],
    ["/example/", "/example/"],
    ["example/", "/example/"],
  ];

  for (const [input, want] of cases) {
    test(`${JSON.stringify(input)} -> ${want}`, () => {
      expect(normalizeBasePath(input)).toBe(want);
    });
  }
});

describe("gestalt vite plugin", () => {
  let tempDirs: string[] = [];
  let viteServers: Array<Awaited<ReturnType<typeof createViteServer>>> = [];
  let occupied: ReturnType<typeof createNetServer> | undefined;

  afterEach(async () => {
    await Promise.all(viteServers.splice(0).map((server) => server.close()));
    if (occupied) {
      await new Promise<void>((resolve, reject) => {
        occupied!.close((err) => (err ? reject(err) : resolve()));
      });
      occupied = undefined;
    }
    for (const dir of tempDirs.splice(0)) {
      removeTempDir(dir);
    }
  });

  test("no env is a no-op via resolveConfig", async () => {
    const config = await resolveConfig(
      {
        configFile: false,
        root: fixtureRoot,
        plugins: [gestalt({ env: {} })],
      },
      "build",
    );
    expect(config.base).toBe("/");
    expect(config.build.outDir).toBe("dist");
  });

  test("dev env applies gestaltd dev contract and /api/v1 proxy", async () => {
    const config = await resolveConfig(
      {
        configFile: false,
        root: fixtureRoot,
        plugins: [
          gestalt({
            env: {
              GESTALT_DEV_PORT: "5173",
              GESTALT_DEV_BASE_PATH: "/example",
              GESTALT_BASE_URL: "http://127.0.0.1:65003",
            },
          }),
        ],
      },
      "serve",
    );
    expect(config.base).toBe("/example/");
    expect(config.server.host).toBe("127.0.0.1");
    expect(config.server.port).toBe(5173);
    expect(config.server.strictPort).toBe(true);
    expect(config.server.allowedHosts).toBe(true);
    expect(config.preview.host).toBe("127.0.0.1");
    expect(config.preview.port).toBe(5173);
    expect(config.preview.strictPort).toBe(true);
    expect(config.preview.allowedHosts).toBe(true);
    expect(config.server.proxy?.["/api/v1"]).toEqual(
      expect.objectContaining({
        target: "http://127.0.0.1:65003",
        changeOrigin: true,
      }),
    );

    const overridden = await resolveConfig(
      {
        configFile: false,
        root: fixtureRoot,
        server: {
          proxy: {
            "/api/v1": { target: "http://127.0.0.1:8080", changeOrigin: true },
          },
        },
        plugins: [
          gestalt({
            env: {
              GESTALT_DEV_PORT: "5173",
              GESTALT_API_PROXY_TARGET: "http://127.0.0.1:9000",
            },
          }),
        ],
      },
      "serve",
    );
    expect(overridden.server.proxy?.["/api/v1"]).toEqual(
      expect.objectContaining({
        target: "http://127.0.0.1:9000",
        changeOrigin: true,
      }),
    );
  });

  test("adds the configured bearer to the API proxy", async () => {
    const config = await resolveConfig(
      {
        configFile: false,
        root: fixtureRoot,
        plugins: [
          gestalt({
            env: {
              GESTALT_DEV_PORT: "5173",
              GESTALT_DEV_API_PROXY_TOKEN: "test-token",
            },
          }),
        ],
      },
      "serve",
    );
    const proxy = config.server.proxy?.["/api/v1"];
    if (!proxy || typeof proxy === "string" || !proxy.configure) {
      throw new Error("expected configurable API proxy");
    }
    const proxyServer = new EventEmitter();
    proxy.configure(proxyServer as never, {} as never);
    let authorization: string | undefined;
    const proxyReq = {
      setHeader: (_name: string, value: string) => (authorization = value),
    };
    proxyServer.emit("proxyReq", proxyReq, {
      headers: { host: "127.0.0.1:5173" },
    });
    expect(authorization).toBe("Bearer test-token");

    expect(authorization).toBe("Bearer test-token");
  });

  test("live dev server serves mount with foreign Host and injected base tag", async () => {
    const cases = [
      {
        basePath: "/example",
        requestPath: "/example/",
        wantBase: '<base href="/example/">',
      },
      { basePath: "/", requestPath: "/", wantBase: '<base href="/">' },
    ] as const;

    for (const { basePath, requestPath, wantBase } of cases) {
      const port = await freePort();
      const env = {
        GESTALT_DEV_PORT: String(port),
        GESTALT_DEV_BASE_PATH: basePath,
      };
      const server = await createViteServer({
        configFile: false,
        root: fixtureRoot,
        plugins: [gestalt({ env })],
      });
      viteServers.push(server);
      await server.listen();

      const response = await fetch(`http://127.0.0.1:${port}${requestPath}`, {
        headers: { Host: "foreign.example.test" },
      });
      expect(response.status).toBe(200);
      const html = await response.text();
      expect(html).toContain(wantBase);
      await server.close();
      viteServers.pop();
    }
  });

  test("build emits relative asset references into GESTALT_BUILD_STATIC", async () => {
    const outDir = makeTempDir("gestalt-vite-test-");
    tempDirs.push(outDir);
    await build({
      configFile: false,
      root: fixtureRoot,
      logLevel: "silent",
      plugins: [gestalt({ env: { GESTALT_BUILD_STATIC: outDir } })],
    });

    const html = readFileSync(join(outDir, "index.html"), "utf8");
    expect(html).not.toMatch(/\ssrc=["']\//);
    expect(html).not.toMatch(/\shref=["']\//);
    expect(html).toMatch(/\ssrc=["']\.\//);
  });

  test("occupied port rejects with strictPort", async () => {
    const port = await freePort();
    occupied = await listen(port);

    const server = await createViteServer({
      configFile: false,
      root: fixtureRoot,
      plugins: [
        gestalt({
          env: {
            GESTALT_DEV_PORT: String(port),
            GESTALT_DEV_BASE_PATH: "/example",
          },
        }),
      ],
    });
    viteServers.push(server);

    await expect(server.listen()).rejects.toThrow();
  });
});
