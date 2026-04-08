import { definePlugin, ok, operation, s } from "../../../src/index.ts";

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
    return {
      name: "fixture-session",
      operations: [
        {
          id: "session-hello",
          method: "GET",
          title: request.connectionParams.scope
            ? `Session Hello ${request.connectionParams.scope}`
            : "Session Hello",
        },
      ],
    };
  },
  operations: [
    operation({
      id: "hello",
      method: "POST",
      title: "Hello",
      description: "Return a greeting",
      tags: ["fixture"],
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
      }),
      handler(input, request) {
        return ok({
          message: `Hello, ${input.name}${input.excited ? "!" : "."}`,
          configuredName,
          region: request.connectionParams.region ?? "",
          configuredRegion: String(configuredConfig.region ?? ""),
        });
      },
    }),
  ],
});
