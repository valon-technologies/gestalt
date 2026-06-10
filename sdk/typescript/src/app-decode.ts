import type { OperationResult } from "./api.ts";
import { InvokeError } from "./invoke_support.ts";

export function operationResult(
  input: Omit<OperationResult, "ok" | "bytes" | "text" | "json" | "requireOk">,
): OperationResult {
  const body = new Uint8Array(input.body);
  return {
    ...input,
    body,
    ok: input.status >= 200 && input.status < 300,
    bytes(): Uint8Array {
      return new Uint8Array(body);
    },
    text(): string {
      return decodeBodyText(body);
    },
    json<T = unknown>(): T {
      return parseOperationResultJson(body) as T;
    },
    requireOk(): OperationResult {
      if (input.status >= 200 && input.status < 300) {
        return this;
      }
      throw new InvokeError({
        status: input.status,
        body: parseOperationResultJsonIfPossible(body),
        rawBody: body,
        message: `app invoke failed with status ${input.status}`,
      });
    },
  };
}

function parseOperationResultJson(body: Uint8Array): unknown {
  const text = decodeBodyText(body);
  if (text.trim() === "") {
    return {};
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new InvokeError({
      message: "operation result body is not valid JSON",
      rawBody: body,
    });
  }
}

function parseOperationResultJsonIfPossible(body: Uint8Array): unknown {
  try {
    return parseOperationResultJson(body);
  } catch {
    return undefined;
  }
}

function decodeBodyText(body: Uint8Array): string {
  return new TextDecoder("utf-8", { fatal: false }).decode(body);
}
