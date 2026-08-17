import { app, tool } from "../../../src/sdk.ts";

type UserStatus = "active" | "disabled";

interface GetUserInput {
  id: string;
  includeHistory?: boolean;
}

interface GetUserOutput {
  id: string;
  displayName: string;
  status: UserStatus;
  labels: string[];
  coordinates: readonly [number, number];
}

export default app({
  tools: {
    getUser: tool({
      description: "Fetch one user.",
      handler: async (input: GetUserInput): Promise<GetUserOutput> => ({
        id: input.id,
        displayName: "Ada Lovelace",
        status: "active",
        labels: input.includeHistory ? ["historical"] : [],
        coordinates: [51.5, -0.1],
      }),
    }),
  },
});
