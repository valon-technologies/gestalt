import type { JsonObject } from "@bufbuild/protobuf";
import type { App } from "./app.ts";
import { decodeGraphQLResult, InvokeError } from "./invoke_support.ts";

/**
 * Options for the {@link invokeGraphQL} helper: the optional invocation
 * targeting fields of the generated `App.invokeGraphQL` surface.
 */
export interface InvokeGraphQLOptions {
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
 * Invokes the GraphQL surface of another app through the generated
 * {@link App} client's `invokeGraphQL` method and decodes the JSON result
 * like {@link decodeGraphQLResult}, throwing {@link InvokeError} when the
 * response carries a GraphQL `errors` array. GraphQL invocation stays a
 * helper by design: the generated method returns the raw result.
 */
export async function invokeGraphQL<T = unknown>(
  client: App,
  app: string,
  document: string,
  options: InvokeGraphQLOptions = {},
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
