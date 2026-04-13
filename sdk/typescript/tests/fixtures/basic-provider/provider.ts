import {
  connectionParam,
  definePlugin,
  ok,
  operation,
  requestCredential,
  requestSubject,
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
    const subject = requestSubject(request);
    const credential = requestCredential(request);
    return {
      name: "fixture-session",
      operations: [
        {
          id: "session-hello",
          method: "GET",
          title: scope
            ? `Session Hello ${scope} ${subject.id} ${credential.mode}`.trim()
            : `Session Hello ${subject.id} ${credential.mode}`.trim(),
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
        subjectId: s.string(),
        credentialMode: s.string(),
      }),
      handler(input, request) {
        const region = connectionParam(request, "region") ?? "";
        const subject = requestSubject(request);
        const credential = requestCredential(request);
        return ok({
          message: `Hello, ${input.name}${input.excited ? "!" : "."}`,
          configuredName,
          region,
          configuredRegion: String(configuredConfig.region ?? ""),
          subjectId: subject.id,
          credentialMode: credential.mode,
        });
      },
    }),
  ],
});
