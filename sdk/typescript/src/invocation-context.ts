import type { Request } from "./api.ts";

export function hostInvocationContext(requestOrToken: Request | string) {
  const invocationToken = (typeof requestOrToken === "string" ? requestOrToken : requestOrToken.invocationToken).trim();
  if (typeof requestOrToken === "string") {
    return { invocationToken };
  }
  const providerName = stringValue(requestOrToken.workflow.providerName) || stringValue(requestOrToken.workflow.provider);
  const runId = stringValue(requestOrToken.workflow.runId);
  return providerName && runId
    ? { invocationToken: "", workflow: { provider: providerName, providerName, runId } }
    : { invocationToken };
}

const stringValue = (value: unknown) => (typeof value === "string" ? value.trim() : "");
