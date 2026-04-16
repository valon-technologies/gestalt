import { Code, ConnectError } from "@connectrpc/connect";

import { defineSecretsProvider } from "../../../src/index.ts";

let configuredName = "";
let configuredScope = "";

const provider = defineSecretsProvider({
  displayName: "Fixture Secrets",
  description: "Secrets fixture used by SDK tests",
  configure(name, config) {
    configuredName = name;
    configuredScope = String(config.scope ?? "");
  },
  async getSecret(name) {
    if (name === "db-password") {
      return `${configuredName}:${configuredScope || "default"}:hunter2`;
    }
    if (name === "api-token") {
      return `${configuredName}:${configuredScope || "default"}:token`;
    }
    throw new ConnectError(`secret ${name} not found`, Code.NotFound);
  },
});

export default provider;
