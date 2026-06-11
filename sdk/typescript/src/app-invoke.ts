import type { JsonObject } from "@bufbuild/protobuf";
import { decodeAppResult, decodeGraphQLResult } from "./app-decode.ts";
import type { App } from "./app.ts";
import { InvokeError } from "./invoke-error.ts";

/**
 * Options for {@link invokeJson}: the optional invocation targeting fields of
 * the generated `App.invoke` surface.
 */
export interface AppInvokeOptions {
  /** Connected account id or name to invoke against. */
  connection?: string;
  /** Provider instance id or name to invoke against. */
  instance?: string;
  /** Idempotency key forwarded to the target operation. */
  idempotencyKey?: string;
  /** Credential mode requested for the target operation. */
  credentialMode?: string;
}

/**
 * Options for {@link invokeGraphQLJson}: the optional invocation targeting
 * fields of the generated `App.invokeGraphQL` surface.
 */
export interface AppInvokeGraphQLOptions {
  /** Connected account id or name to invoke against. */
  connection?: string;
  /** Provider instance id or name to invoke against. */
  instance?: string;
  /** Idempotency key forwarded to the target operation. */
  idempotencyKey?: string;
  /** GraphQL variables for the document. */
  variables?: object;
}

/**
 * Invokes one operation through the generated {@link App} client and decodes
 * the JSON result with the standard envelope semantics of
 * {@link decodeAppResult}: `{status:"success",data}` envelopes return `data`,
 * error envelopes and HTTP-error statuses throw {@link InvokeError}.
 */
export async function invokeJson<T = unknown>(
  client: App,
  app: string,
  operation: string,
  params: object = {},
  options: AppInvokeOptions = {},
): Promise<T> {
  const result = await client.invoke(
    app,
    operation,
    options.connection ?? "",
    options.instance ?? "",
    options.idempotencyKey?.trim() ?? "",
    options.credentialMode?.trim() ?? "",
    toJsonObject(params),
  );
  return decodeAppResult<T>(app, operation, result);
}

/**
 * Invokes the GraphQL surface of another app through the generated
 * {@link App} client and decodes the JSON result like
 * {@link decodeGraphQLResult}, throwing {@link InvokeError} when the response
 * carries a GraphQL `errors` array.
 */
export async function invokeGraphQLJson<T = unknown>(
  client: App,
  app: string,
  document: string,
  options: AppInvokeGraphQLOptions = {},
): Promise<T> {
  const trimmedDocument = document.trim();
  if (!trimmedDocument) {
    throw new InvokeError({
      app,
      operation: "graphql",
      message: "graphql document is required",
    });
  }
  const result = await client.invokeGraphQL(
    app,
    trimmedDocument,
    options.connection ?? "",
    options.instance ?? "",
    options.idempotencyKey?.trim() ?? "",
    options.variables !== undefined ? toJsonObject(options.variables) : undefined,
  );
  return decodeGraphQLResult<T>(app, result);
}

function toJsonObject(params: object): JsonObject {
  return JSON.parse(JSON.stringify(params)) as JsonObject;
}
