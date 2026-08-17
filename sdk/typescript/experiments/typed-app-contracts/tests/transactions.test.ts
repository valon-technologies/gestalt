import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, test } from "bun:test";

import { ExperimentError } from "../src/model.ts";
import { FilesystemRegistry, addDependency, initializeProject, readProjectManifest } from "../src/registry.ts";
import { InstallationManager, type InvocationRoute } from "../src/runtime.ts";
import { linkZod, manifest, sdkPath } from "./helpers.ts";

const transactionsPath = join(import.meta.dir, "..", "src", "transactions.ts");

interface Account {
  id: string;
  balance: number;
}

type TransactionRequest =
  | { op: "begin"; scope: string[]; mode: "readonly" | "readwrite" }
  | { op: "get"; transactionId: string; sequence: number; store: "accounts"; key: string }
  | { op: "put"; transactionId: string; sequence: number; store: "accounts"; value: Account }
  | { op: "delete"; transactionId: string; sequence: number; store: "accounts"; key: string }
  | { op: "commit"; transactionId: string; sequence: number; nonce: string }
  | { op: "abort"; transactionId: string; sequence: number }
  | { op: "status"; transactionId: string; nonce: string };

interface Session {
  mode: "readonly" | "readwrite";
  nextSequence: number;
  accounts: Map<string, Account>;
}

class MemoryTransactionCoordinator {
  readonly commands: TransactionRequest[] = [];
  readonly sessions = new Map<string, Session>();
  readonly receipts = new Map<string, "committed" | "aborted">();
  readonly accounts = new Map<string, Account>([
    ["from", { id: "from", balance: 100 }],
    ["to", { id: "to", balance: 10 }],
  ]);
  loseNextCommitAcknowledgement = false;
  private nextId = 1;

  async invoke(raw: unknown): Promise<unknown> {
    const request = structuredClone(raw) as TransactionRequest;
    this.commands.push(request);
    const response = this.handle(request);
    if (request.op === "commit" && this.loseNextCommitAcknowledgement) {
      this.loseNextCommitAcknowledgement = false;
      throw new Error("simulated response loss after durable commit");
    }
    return response;
  }

  private handle(request: TransactionRequest): unknown {
    if (request.op === "begin") {
      const transactionId = `tx-${this.nextId}`;
      this.nextId += 1;
      this.sessions.set(transactionId, {
        mode: request.mode,
        nextSequence: 1,
        accounts: cloneAccounts(this.accounts),
      });
      return { op: "opened", transactionId, nextSequence: 1 };
    }
    if (request.op === "status") {
      const outcome = this.receipts.get(request.nonce) ??
        (this.sessions.has(request.transactionId) ? "active" : "unknown");
      return { op: "status", transactionId: request.transactionId, nonce: request.nonce, outcome };
    }

    const session = this.sessions.get(request.transactionId);
    if (!session) throw new Error(`unknown transaction ${request.transactionId}`);
    if (request.sequence !== session.nextSequence) {
      throw new Error(`expected sequence ${session.nextSequence}, received ${request.sequence}`);
    }

    if (request.op === "get") {
      session.nextSequence += 1;
      return {
        op: "value",
        transactionId: request.transactionId,
        sequence: request.sequence,
        value: structuredClone(session.accounts.get(request.key) ?? null),
      };
    }
    if (request.op === "put") {
      if (session.mode !== "readwrite") throw new Error("readonly transaction");
      session.accounts.set(request.value.id, structuredClone(request.value));
      session.nextSequence += 1;
      return {
        op: "written",
        transactionId: request.transactionId,
        sequence: request.sequence,
        key: request.value.id,
      };
    }
    if (request.op === "delete") {
      if (session.mode !== "readwrite") throw new Error("readonly transaction");
      session.accounts.delete(request.key);
      session.nextSequence += 1;
      return { op: "deleted", transactionId: request.transactionId, sequence: request.sequence };
    }
    if (request.op === "commit") {
      replaceAccounts(this.accounts, session.accounts);
      this.sessions.delete(request.transactionId);
      this.receipts.set(request.nonce, "committed");
      return { op: "committed", transactionId: request.transactionId, nonce: request.nonce };
    }
    this.sessions.delete(request.transactionId);
    return { op: "aborted", transactionId: request.transactionId };
  }
}

describe("IndexedDB-style transactions over unary tools", () => {
  test("R-TXN-01 exposes transaction(callback) while publishing only one ordinary tool", async () => {
    const coordinator = new MemoryTransactionCoordinator();
    const fixture = await publishTransactionGraph(coordinator);

    expect(Object.keys(fixture.datastore.contract.tools)).toEqual(["transaction"]);
    expect(await fixture.installation.invoke("transfer", { amount: 25 })).toEqual({
      fromBalance: 75,
      toBalance: 35,
    });
    expect(coordinator.accounts.get("from")?.balance).toBe(75);
    expect(coordinator.accounts.get("to")?.balance).toBe(35);
  });

  test("R-TXN-02 preserves one ordered transaction when commands hit different App replicas", async () => {
    const coordinator = new MemoryTransactionCoordinator();
    const fixture = await publishTransactionGraph(coordinator);

    await fixture.installation.invoke("transfer", { amount: 25 });
    const datastoreRoutes = fixture.routes
      .filter((route) => route.app === "acme/datastore")
      .map((route) => route.replica);
    expect(datastoreRoutes).toEqual([0, 1, 2, 0, 1, 2]);

    const commands = coordinator.commands.filter((command) => command.op !== "begin");
    const transactionIds = new Set(commands.map((command) => command.transactionId));
    expect(transactionIds.size).toBe(1);
    expect(commands.flatMap((command) => "sequence" in command ? [command.sequence] : [])).toEqual([
      1, 2, 3, 4, 5,
    ]);
  });

  test("R-TXN-03 aborts failed callbacks and recovers a lost commit acknowledgement", async () => {
    const coordinator = new MemoryTransactionCoordinator();
    const fixture = await publishTransactionGraph(coordinator);

    try {
      await fixture.installation.invoke("failTransfer", { amount: 25 });
      throw new Error("expected callback failure");
    } catch (error) {
      expect(error).toBeInstanceOf(ExperimentError);
      expect((error as ExperimentError).code).toBe("HANDLER_FAILED");
    }
    expect(coordinator.accounts.get("from")?.balance).toBe(100);
    expect(coordinator.commands.at(-1)?.op).toBe("abort");

    coordinator.loseNextCommitAcknowledgement = true;
    expect(await fixture.installation.invoke("transfer", { amount: 10 })).toEqual({
      fromBalance: 90,
      toBalance: 20,
    });
    expect(coordinator.commands.slice(-2).map((command) => command.op)).toEqual(["commit", "status"]);
    expect(coordinator.sessions.size).toBe(0);
  });
});

async function publishTransactionGraph(coordinator: MemoryTransactionCoordinator): Promise<{
  datastore: Awaited<ReturnType<FilesystemRegistry["publish"]>>;
  installation: InstallationManager;
  routes: InvocationRoute[];
}> {
  const coordinatorGlobal = globalThis as unknown as {
    __gestaltExperimentTransactionCoordinator?: (input: unknown) => Promise<unknown>;
  };
  coordinatorGlobal.__gestaltExperimentTransactionCoordinator = async (input) =>
    await coordinator.invoke(input);

  const root = await mkdtemp(join(tmpdir(), "gestalt-transaction-"));
  await linkZod(root);
  const registry = new FilesystemRegistry(join(root, "registry"));

  const datastoreSource = join(root, "datastore.ts");
  await writeFile(datastoreSource, datastoreSourceText());
  const datastore = await registry.publish(datastoreSource, manifest("acme/datastore", "1.0.0"));

  const consumerProject = join(root, "consumer");
  await initializeProject(consumerProject, manifest("acme/consumer", "1.0.0"));
  await addDependency({
    registry,
    projectDirectory: consumerProject,
    alias: "datastore",
    app: "acme/datastore",
    version: "1.0.0",
  });
  const consumerSource = join(consumerProject, "app.ts");
  await writeFile(consumerSource, consumerSourceText());
  await registry.publish(consumerSource, await readProjectManifest(consumerProject));

  const routes: InvocationRoute[] = [];
  const installation = new InstallationManager(registry, {
    replicasPerRelease: 3,
    onRoute: (route) => routes.push(route),
  });
  await installation.activate("acme/consumer", "1.0.0");
  return { datastore, installation, routes };
}

function datastoreSourceText(): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};

    const Account = z.strictObject({ id: z.string(), balance: z.number().int() });
    const TransactionInput = z.union([
      z.strictObject({
        op: z.literal("begin"),
        scope: z.array(z.literal("accounts")),
        mode: z.enum(["readonly", "readwrite"]),
      }),
      z.strictObject({
        op: z.literal("get"), transactionId: z.string(), sequence: z.number().int().min(0),
        store: z.literal("accounts"), key: z.string(),
      }),
      z.strictObject({
        op: z.literal("put"), transactionId: z.string(), sequence: z.number().int().min(0),
        store: z.literal("accounts"), value: Account,
      }),
      z.strictObject({
        op: z.literal("delete"), transactionId: z.string(), sequence: z.number().int().min(0),
        store: z.literal("accounts"), key: z.string(),
      }),
      z.strictObject({
        op: z.literal("commit"), transactionId: z.string(), sequence: z.number().int().min(0),
        nonce: z.string(),
      }),
      z.strictObject({
        op: z.literal("abort"), transactionId: z.string(), sequence: z.number().int().min(0),
      }),
      z.strictObject({ op: z.literal("status"), transactionId: z.string(), nonce: z.string() }),
    ]);
    const TransactionOutput = z.union([
      z.strictObject({
        op: z.literal("opened"), transactionId: z.string(), nextSequence: z.number().int().min(0),
      }),
      z.strictObject({
        op: z.literal("value"), transactionId: z.string(), sequence: z.number().int().min(0),
        value: Account.nullable(),
      }),
      z.strictObject({
        op: z.literal("written"), transactionId: z.string(), sequence: z.number().int().min(0),
        key: z.string(),
      }),
      z.strictObject({
        op: z.literal("deleted"), transactionId: z.string(), sequence: z.number().int().min(0),
      }),
      z.strictObject({ op: z.literal("committed"), transactionId: z.string(), nonce: z.string() }),
      z.strictObject({ op: z.literal("aborted"), transactionId: z.string() }),
      z.strictObject({
        op: z.literal("status"), transactionId: z.string(), nonce: z.string(),
        outcome: z.enum(["active", "committed", "aborted", "unknown"]),
      }),
    ]);

    declare global {
      var __gestaltExperimentTransactionCoordinator:
        | ((input: z.infer<typeof TransactionInput>) => Promise<z.infer<typeof TransactionOutput>>)
        | undefined;
    }

    export default app({ tools: {
      transaction: tool({
        description: "Execute one command in an ordered database transaction.",
        input: TransactionInput,
        output: TransactionOutput,
        handler: async (input) => {
          const invoke = globalThis.__gestaltExperimentTransactionCoordinator;
          if (!invoke) throw new Error("transaction coordinator is unavailable");
          return await invoke(input);
        },
      }),
    } });
  `;
}

function consumerSourceText(): string {
  return `
    import { z } from "zod";
    import { app, tool } from ${JSON.stringify(sdkPath)};
    import { database } from ${JSON.stringify(transactionsPath)};
    import { transaction } from "@gestalt/apps/datastore";

    interface Account { id: string; balance: number }
    const State = database<{ accounts: Account }>(transaction);
    const TransferInput = z.strictObject({ amount: z.number().int().min(1) });
    const TransferOutput = z.strictObject({ fromBalance: z.number().int(), toBalance: z.number().int() });

    async function transfer(amount: number, fail: boolean) {
      return await State.transaction(["accounts"], "readwrite", async ({ accounts }) => {
        const from = await accounts.get("from");
        const to = await accounts.get("to");
        if (!from || !to || from.balance < amount) throw new Error("invalid transfer");
        await accounts.put({ ...from, balance: from.balance - amount });
        if (fail) throw new Error("intentional callback failure");
        await accounts.put({ ...to, balance: to.balance + amount });
        return { fromBalance: from.balance - amount, toBalance: to.balance + amount };
      });
    }

    export default app({ tools: {
      transfer: tool({
        input: TransferInput,
        output: TransferOutput,
        handler: async ({ amount }) => await transfer(amount, false),
      }),
      failTransfer: tool({
        input: TransferInput,
        output: TransferOutput,
        handler: async ({ amount }) => await transfer(amount, true),
      }),
    } });
  `;
}

function cloneAccounts(source: Map<string, Account>): Map<string, Account> {
  return new Map([...source].map(([key, value]) => [key, structuredClone(value)]));
}

function replaceAccounts(target: Map<string, Account>, source: Map<string, Account>): void {
  target.clear();
  for (const [key, value] of source) target.set(key, structuredClone(value));
}
