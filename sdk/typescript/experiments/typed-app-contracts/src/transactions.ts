export type TransactionMode = "readonly" | "readwrite";

export interface TransactionStore<Value> {
  get(key: string): Promise<Value | undefined>;
  put(value: Value): Promise<string>;
  delete(key: string): Promise<void>;
}

export type TransactionStores<Stores extends object> = {
  [Name in keyof Stores]: TransactionStore<Stores[Name]>;
};

export interface Database<Stores extends object> {
  transaction<const Name extends Extract<keyof Stores, string>, Result>(
    scope: readonly Name[],
    mode: TransactionMode,
    callback: (stores: Pick<TransactionStores<Stores>, Name>) => Promise<Result>,
  ): Promise<Result>;
}

export interface DatabaseOptions {
  nonce?: () => string;
}

type TransactionTool = (input: any) => Promise<any>;

/**
 * Creates the local API presented by a Datastore-style App. The transport remains one
 * ordinary unary tool; session identifiers and command sequencing stay behind this facade.
 */
export function database<Stores extends object>(
  invoke: TransactionTool,
  options: DatabaseOptions = {},
): Database<Stores> {
  const nonce = options.nonce ?? (() => globalThis.crypto.randomUUID());

  return {
    async transaction(scope, mode, callback) {
      const names = scope.map(String);
      if (names.length === 0 || new Set(names).size !== names.length) {
        throw new TransactionProtocolError("transaction scope must be non-empty and unique");
      }

      const opened = expectReply(
        await invoke({ op: "begin", scope: names, mode }),
        "opened",
      );
      const transactionId = stringField(opened, "transactionId");
      let nextSequence = integerField(opened, "nextSequence");
      let terminal = false;
      let queue: Promise<void> = Promise.resolve();

      const command = <Result>(
        op: "get" | "put" | "delete",
        fields: Record<string, unknown>,
        decode: (reply: Record<string, unknown>) => Result,
      ): Promise<Result> => {
        if (terminal) {
          return Promise.reject(new TransactionProtocolError("transaction is already complete"));
        }
        const result = queue.then(async () => {
          const sequence = nextSequence;
          const reply = expectReply(
            await invoke({ op, transactionId, sequence, ...fields }),
            op === "get" ? "value" : op === "put" ? "written" : "deleted",
          );
          if (
            stringField(reply, "transactionId") !== transactionId ||
            integerField(reply, "sequence") !== sequence
          ) {
            throw new TransactionProtocolError("transaction reply does not match its command");
          }
          nextSequence += 1;
          return decode(reply);
        });
        queue = result.then(() => undefined);
        return result;
      };

      const stores = Object.fromEntries(
        names.map((name) => [
          name,
          {
            get: async (key: string) =>
              await command("get", { store: name, key }, (reply) =>
                reply.value === null ? undefined : reply.value,
              ),
            put: async (value: unknown) => {
              if (mode !== "readwrite") {
                throw new TransactionProtocolError("put requires a readwrite transaction");
              }
              return await command("put", { store: name, value }, (reply) =>
                stringField(reply, "key"),
              );
            },
            delete: async (key: string) => {
              if (mode !== "readwrite") {
                throw new TransactionProtocolError("delete requires a readwrite transaction");
              }
              await command("delete", { store: name, key }, () => undefined);
            },
          },
        ]),
      ) as Pick<TransactionStores<Stores>, (typeof scope)[number]>;

      try {
        const result = await callback(stores);
        await queue;
        terminal = true;
        await commit(invoke, transactionId, nextSequence, nonce());
        return result;
      } catch (error) {
        await queue.catch(() => undefined);
        if (!terminal) {
          terminal = true;
          await abort(invoke, transactionId, nextSequence).catch(() => undefined);
        }
        throw error;
      }
    },
  };
}

async function commit(
  invoke: TransactionTool,
  transactionId: string,
  sequence: number,
  nonce: string,
): Promise<void> {
  try {
    const reply = expectReply(
      await invoke({ op: "commit", transactionId, sequence, nonce }),
      "committed",
    );
    if (
      stringField(reply, "transactionId") !== transactionId ||
      stringField(reply, "nonce") !== nonce
    ) {
      throw new TransactionProtocolError("commit reply does not match its command");
    }
  } catch (commitError) {
    let status: Record<string, unknown>;
    try {
      status = expectReply(
        await invoke({ op: "status", transactionId, nonce }),
        "status",
      );
    } catch {
      throw new TransactionOutcomeUnknownError(transactionId, nonce, commitError);
    }
    const outcome = stringField(status, "outcome");
    if (outcome === "committed") return;
    if (outcome === "active") {
      await abort(invoke, transactionId, sequence).catch(() => undefined);
      throw commitError;
    }
    if (outcome === "aborted") throw commitError;
    throw new TransactionOutcomeUnknownError(transactionId, nonce, commitError);
  }
}

async function abort(
  invoke: TransactionTool,
  transactionId: string,
  sequence: number,
): Promise<void> {
  const reply = expectReply(
    await invoke({ op: "abort", transactionId, sequence }),
    "aborted",
  );
  if (stringField(reply, "transactionId") !== transactionId) {
    throw new TransactionProtocolError("abort reply does not match its command");
  }
}

function expectReply(value: unknown, op: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new TransactionProtocolError(`expected ${op} reply`);
  }
  const reply = value as Record<string, unknown>;
  if (reply.op !== op) throw new TransactionProtocolError(`expected ${op} reply`);
  return reply;
}

function stringField(value: Record<string, unknown>, field: string): string {
  const result = value[field];
  if (typeof result !== "string") {
    throw new TransactionProtocolError(`${field} must be a string`);
  }
  return result;
}

function integerField(value: Record<string, unknown>, field: string): number {
  const result = value[field];
  if (!Number.isSafeInteger(result) || (result as number) < 0) {
    throw new TransactionProtocolError(`${field} must be a non-negative safe integer`);
  }
  return result as number;
}

export class TransactionProtocolError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TransactionProtocolError";
  }
}

export class TransactionOutcomeUnknownError extends Error {
  constructor(
    readonly transactionId: string,
    readonly nonce: string,
    readonly cause: unknown,
  ) {
    super(`commit outcome is unknown for ${transactionId}`);
    this.name = "TransactionOutcomeUnknownError";
  }
}
