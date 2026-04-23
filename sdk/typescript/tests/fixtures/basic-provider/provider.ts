import {
  connectionParam,
  definePlugin,
  ok,
  s,
} from "../../../src/index.ts";

let configuredName = "";
let configuredConfig: Record<string, unknown> = {};

export const plugin = definePlugin({
  displayName: "Fixture Provider",
  description: "Provider fixture used by SDK tests",
  configure(name, config) {
    configuredName = name;
    configuredConfig = {
      ...config,
    };
  },
  sessionCatalog(request) {
    const scope = connectionParam(request, "scope");
    return {
      name: "fixture-session",
      operations: [
        {
          id: "session-hello",
          method: "GET",
          allowedRoles: ["viewer", "admin"],
          title: scope
            ? `Session Hello ${scope} ${request.subject.id} ${request.credential.mode} ${request.access.role}`.trim()
            : `Session Hello ${request.subject.id} ${request.credential.mode} ${request.access.role}`.trim(),
        },
      ],
    };
  },
  postConnect(token) {
    return {
      "gestalt.external_identity.type": "fixture_identity",
      "gestalt.external_identity.id": `${token.connection}:${token.instance}:${token.subjectId}`,
      configured_connection: token.connection,
    };
  },
  operations: [
    {
      id: "  hello  ",
      method: " post ",
      title: "  Hello  ",
      description: "  Return a greeting  ",
      tags: ["fixture"],
      allowedRoles: ["viewer", "admin", "viewer"],
      readOnly: true,
      input: s.object({
        name: s.string({
          description: "Name to greet",
          default: "World",
        }),
        excited: s.optional(
          s.boolean({
            description: "Add punctuation",
          }),
        ),
      }),
      output: s.object({
        message: s.string(),
        configuredName: s.string(),
        region: s.string(),
        configuredRegion: s.string(),
        subjectId: s.string(),
        credentialMode: s.string(),
        accessPolicy: s.string(),
        accessRole: s.string(),
        invocationToken: s.string(),
      }),
      handler(input, request) {
        const region = connectionParam(request, "region") ?? "";
        return ok({
          message: `Hello, ${input.name}${input.excited ? "!" : "."}`,
          configuredName,
          region,
          configuredRegion: String(configuredConfig.region ?? ""),
          subjectId: request.subject.id,
          credentialMode: request.credential.mode,
          accessPolicy: request.access.policy,
          accessRole: request.access.role,
          invocationToken: request.invocationToken,
        });
      },
    },
    {
      id: " count ",
      method: " post ",
      title: " Count ",
      description: " Echo an integer count ",
      input: s.object({
        count: s.integer(),
      }),
      output: s.object({
        count: s.integer(),
      }),
      handler(input) {
        return ok({
          count: input.count,
        });
      },
    },
  ],
});
