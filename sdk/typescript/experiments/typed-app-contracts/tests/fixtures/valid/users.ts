import { z } from "zod";

import { app, tool } from "../../../src/sdk.ts";

const GetUserInput = z.strictObject({
  id: z.string(),
  includeHistory: z.boolean().optional(),
});

const GetUserOutput = z.strictObject({
  id: z.string(),
  displayName: z.string(),
  status: z.enum(["active", "disabled"]),
  labels: z.array(z.string()),
  score: z.number().min(0).max(100),
});

export default app({
  tools: {
    getUser: tool({
      description: "Fetch one user.",
      input: GetUserInput,
      output: GetUserOutput,
      handler: async (input) => ({
        id: input.id,
        displayName: "Ada Lovelace",
        status: "active" as const,
        labels: input.includeHistory ? ["historical"] : [],
        score: 100,
      }),
    }),
  },
});
