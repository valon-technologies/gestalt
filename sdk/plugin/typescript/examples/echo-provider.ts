import {
  serveProvider,
  ConnectionMode,
  type ProviderDefinition,
} from "../src";

const definition: ProviderDefinition = {
  metadata: {
    name: "echo",
    displayName: "Echo",
    description: "Echoes back the input parameters",
    connectionMode: ConnectionMode.NONE,
    authTypes: [],
    connectionParams: {},
    staticCatalogJson: "",
    supportsSessionCatalog: false,
    supportsPostConnect: false,
  },
  operations: [
    {
      name: "echo",
      description: "Echo back input params as JSON",
      method: "POST",
      parameters: [],
    },
  ],
  async execute(request) {
    const params = request.params?.fields ?? {};

    const plain: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(params)) {
      if (value.kind.oneofKind === "stringValue") {
        plain[key] = value.kind.stringValue;
      } else if (value.kind.oneofKind === "numberValue") {
        plain[key] = value.kind.numberValue;
      } else if (value.kind.oneofKind === "boolValue") {
        plain[key] = value.kind.boolValue;
      } else if (value.kind.oneofKind === "nullValue") {
        plain[key] = null;
      }
    }

    return {
      status: 200,
      body: JSON.stringify(plain),
    };
  },
};

serveProvider(definition);
