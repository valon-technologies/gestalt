import { readFileSync } from "node:fs";
import { join } from "node:path";

import { create } from "@bufbuild/protobuf";
import { EmptySchema } from "@bufbuild/protobuf/wkt";
import { expect, test } from "bun:test";

import {
  CompleteLoginRequestSchema,
  ValidateExternalTokenRequestSchema,
} from "../gen/v1/auth_pb.ts";
import {
  CreateBlobRequestSchema,
  CreateObjectURLRequestSchema,
  FileObjectRequestSchema,
  ObjectURLRequestSchema,
  ReadStreamRequestSchema,
} from "../gen/v1/fileapi_pb.ts";
import {
  AccessContextSchema,
  CredentialContextSchema,
  ExecuteRequestSchema,
  GetSessionCatalogRequestSchema,
  RequestContextSchema,
  StartProviderRequestSchema,
  SubjectContextSchema,
} from "../gen/v1/plugin_pb.ts";
import { ConfigureProviderRequestSchema } from "../gen/v1/runtime_pb.ts";
import {
  ENV_WRITE_CATALOG,
  createAuthService,
  createFileAPIService,
  createProviderService,
  createRuntimeService,
  loadPluginFromTarget,
  loadProviderFromTarget,
  main,
  parseRuntimeArgs,
} from "../src/runtime.ts";
import { fixturePath, makeTempDir, removeTempDir } from "./helpers.ts";

test("runtime arg parsing requires root and target", () => {
  expect(parseRuntimeArgs(["root", "plugin:./provider.ts#plugin"])).toEqual({
    root: "root",
    target: "plugin:./provider.ts#plugin",
  });
  expect(parseRuntimeArgs(["root"])).toBeUndefined();
});

test("runtime main writes a static catalog in catalog mode", async () => {
  const root = fixturePath("basic-provider");
  const tempDir = makeTempDir();
  const catalogPath = join(tempDir, "catalog.yaml");
  const previousCatalog = process.env[ENV_WRITE_CATALOG];

  process.env[ENV_WRITE_CATALOG] = catalogPath;
  try {
    const code = await main([root, "plugin:./provider.ts#plugin"]);
    expect(code).toBe(0);
    const catalog = readFileSync(catalogPath, "utf8");
    expect(catalog).toContain("name: basic-provider");
    expect(catalog).toContain("id: hello");
  } finally {
    if (previousCatalog === undefined) {
      delete process.env[ENV_WRITE_CATALOG];
    } else {
      process.env[ENV_WRITE_CATALOG] = previousCatalog;
    }
    removeTempDir(tempDir);
  }
});

test("integration provider service exposes metadata, configure, execute, and session catalog", async () => {
  const plugin = await loadPluginFromTarget(fixturePath("basic-provider"));
  const service = createProviderService(plugin);

  const metadata = await (service.getMetadata as any)();
  expect(metadata.name).toBe("basic-provider");
  expect(metadata.supportsSessionCatalog).toBe(true);
  expect(
    metadata.staticCatalog?.operations?.some((op: any) => op.id === "hello"),
  ).toBe(true);
  expect(
    metadata.staticCatalog?.operations?.find((op: any) => op.id === "hello")
      ?.allowedRoles,
  ).toEqual(["viewer", "admin"]);

  await (service.startProvider as any)(
    create(StartProviderRequestSchema, {
      name: "configured-provider",
      config: {
        region: "use1",
      },
    }),
  );

  const result = await (service.execute as any)(
    create(ExecuteRequestSchema, {
      operation: "hello",
      params: {
        name: "Ada",
      },
      token: "token-123",
      connectionParams: {
        region: "iad",
      },
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, {
          id: "user:user-123",
          kind: "user",
          authSource: "api_token",
        }),
        credential: create(CredentialContextSchema, {
          mode: "identity",
          subjectId: "identity:__identity__",
        }),
        access: create(AccessContextSchema, {
          policy: "sample_policy",
          role: "admin",
        }),
      }),
    }),
  );
  expect(JSON.parse(result.body)).toEqual({
    message: "Hello, Ada.",
    configuredName: "configured-provider",
    region: "iad",
    configuredRegion: "use1",
    subjectId: "user:user-123",
    credentialMode: "identity",
    accessPolicy: "sample_policy",
    accessRole: "admin",
  });

  const sessionCatalog = await (service.getSessionCatalog as any)(
    create(GetSessionCatalogRequestSchema, {
      token: "token-123",
      connectionParams: {
        scope: "ops",
      },
      context: create(RequestContextSchema, {
        subject: create(SubjectContextSchema, {
          id: "user:user-123",
          kind: "user",
        }),
        credential: create(CredentialContextSchema, {
          mode: "identity",
        }),
        access: create(AccessContextSchema, {
          policy: "sample_policy",
          role: "viewer",
        }),
      }),
    }),
  );
  expect(sessionCatalog.catalog?.name).toBe("fixture-session");
  expect(sessionCatalog.catalog?.operations).toHaveLength(1);
  expect(sessionCatalog.catalog?.operations[0].id).toBe("session-hello");
  expect(sessionCatalog.catalog?.operations[0].method).toBe("GET");
  expect(sessionCatalog.catalog?.operations[0].allowedRoles).toEqual([
    "viewer",
    "admin",
  ]);
  expect(sessionCatalog.catalog?.operations[0].title).toBe(
    "Session Hello ops user:user-123 identity viewer",
  );
});

test("auth provider supports runtime metadata, login flows, and token validation", async () => {
  const provider = await loadProviderFromTarget(fixturePath("auth-provider"));
  const runtime = createRuntimeService(provider);
  const auth = createAuthService(provider as any);

  await (runtime.configureProvider as any)(
    create(ConfigureProviderRequestSchema, {
      name: "fixture-auth",
      config: {
        issuer: "https://login.example.test",
      },
      protocolVersion: 2,
    }),
  );

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(2);
  expect(metadata.displayName).toBe("Fixture Auth");

  const begin = await (auth.beginLogin as any)(
    create((await import("../gen/v1/auth_pb.ts")).BeginLoginRequestSchema, {
      callbackUrl: "https://app.example.test/callback",
      hostState: "host-state",
      scopes: ["openid"],
    }),
  );
  expect(begin.authorizationUrl).toContain(
    "https://login.example.test/authorize",
  );

  const user = await (auth.completeLogin as any)(
    create(CompleteLoginRequestSchema, {
      query: {
        code: "code-123",
      },
      callbackUrl: "https://app.example.test/callback",
      providerState: new Uint8Array([1, 2, 3]),
    }),
  );
  expect(user.subject).toBe("code-123");
  expect(user.claims.issuer).toBe("https://login.example.test");

  const validated = await (auth.validateExternalToken as any)(
    create(ValidateExternalTokenRequestSchema, {
      token: "api-token",
    }),
  );
  expect(validated.email).toBe("api-token@example.com");
});

test("fileapi provider supports runtime metadata and file operations", async () => {
  const provider = await loadProviderFromTarget(fixturePath("fileapi-provider"));
  const runtime = createRuntimeService(provider);
  const fileapi = createFileAPIService(provider as any);

  const metadata = await (runtime.getProviderIdentity as any)(
    create(EmptySchema, {}),
  );
  expect(metadata.kind).toBe(6);
  expect(metadata.displayName).toBe("Fixture FileAPI");

  const blob = await (fileapi.createBlob as any)(
    create(CreateBlobRequestSchema, {
      parts: [],
      options: {
        mimeType: "text/plain",
      },
    }),
  );
  expect(blob.object?.id).toBe("blob:0");

  const stat = await (fileapi.stat as any)(
    create(FileObjectRequestSchema, {
      id: "blob:0",
    }),
  );
  expect(stat.object?.name).toBe("fixture.txt");

  const bytes = await (fileapi.readBytes as any)(
    create(FileObjectRequestSchema, {
      id: "blob:0",
    }),
  );
  expect(new TextDecoder().decode(bytes.data)).toBe("bytes:blob:0");

  const chunks: string[] = [];
  for await (const chunk of (fileapi.openReadStream as any)(
    create(ReadStreamRequestSchema, {
      id: "blob:0",
      chunkSize: 4,
    }),
  )) {
    chunks.push(new TextDecoder().decode(chunk.data));
  }
  expect(chunks).toEqual(["blob:0:chunk-1", "blob:0:chunk-2"]);

  const objectURL = await (fileapi.createObjectURL as any)(
    create(CreateObjectURLRequestSchema, {
      id: "blob:0",
    }),
  );
  expect(objectURL.url).toBe("memory://blob:0");

  const resolved = await (fileapi.resolveObjectURL as any)(
    create(ObjectURLRequestSchema, {
      url: objectURL.url,
    }),
  );
  expect(resolved.object?.id).toBe("blob:0");

  const revoked = await (fileapi.revokeObjectURL as any)(
    create(ObjectURLRequestSchema, {
      url: objectURL.url,
    }),
  );
  expect(revoked).toEqual(create(EmptySchema, {}));
});

test("fileapi provider rejects plain-object exports without runtime provider methods", async () => {
  await expect(
    loadProviderFromTarget(fixturePath("fileapi-provider-missing-revoke")),
  ).rejects.toThrow(
    "did not resolve to a Gestalt FileAPI provider",
  );
});
