import { definePlugin, ok, operation, s } from "../../../src/index.ts";

const provider = definePlugin({
  displayName: "Fixture Provider Default Export",
  operations: [
    operation({
      id: "hello",
      method: "POST",
      input: s.object({
        name: s.string({
          default: "World",
        }),
      }),
      output: s.object({
        message: s.string(),
      }),
      handler(input) {
        return ok({
          message: `Hello, ${input.name}.`,
        });
      },
    }),
  ],
});

export default provider;
