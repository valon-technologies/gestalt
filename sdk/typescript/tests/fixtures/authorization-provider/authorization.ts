import { defineAuthorizationProvider } from "../../../src/index.ts";

let resourcePrefix = "fixture";

export const provider = defineAuthorizationProvider({
  displayName: "Fixture Authorization",
  description: "Authorization fixture used by SDK tests",
  configure(_name, config) {
    resourcePrefix = String(config.prefix ?? resourcePrefix);
  },
  evaluate(request) {
    return {
      allowed: request.subject?.id === "user:1",
      modelId: "authz-model-1",
    };
  },
  evaluateMany(request) {
    return {
      decisions: (request.requests ?? []).map((entry) => ({
        allowed: entry.subject?.id === "user:1",
        modelId: "authz-model-1",
      })),
    };
  },
  searchResources(request) {
    return {
      resources: [
        {
          type: request.resourceType ?? "",
          id: `${resourcePrefix}-doc-1`,
        },
      ],
      modelId: "authz-model-1",
    };
  },
  searchSubjects(request) {
    return {
      subjects: [
        {
          type: request.subjectType ?? "",
          id: "user:1",
        },
      ],
      modelId: "authz-model-1",
    };
  },
  searchActions() {
    return {
      actions: [{ name: "view" }],
      modelId: "authz-model-1",
    };
  },
  getMetadata() {
    return {
      capabilities: ["evaluate"],
      activeModelId: "authz-model-1",
    };
  },
  readRelationships() {
    return {
      relationships: [],
      modelId: "authz-model-1",
    };
  },
  writeRelationships() {},
  getActiveModel() {
    return {
      model: {
        id: "authz-model-1",
        version: "1",
      },
    };
  },
  listModels() {
    return {
      models: [
        {
          id: "authz-model-1",
          version: "1",
        },
      ],
    };
  },
  writeModel(request) {
    return {
      id: "authz-model-2",
      version: String(request.model?.version ?? ""),
    };
  },
});
