import { describe, expect, test } from "bun:test";

import {
  Agent,
  type AgentTurnEvent,
} from "../src/client/index.ts";

describe("public Agent client", () => {
  test("creates, sends a string message, and awaits a durable result", async () => {
    const requests: Array<{ url: string; init?: RequestInit }> = [];
    let runReads = 0;
    const fetch = async (
      input: string | URL | Request,
      init?: RequestInit,
    ): Promise<Response> => {
      const url = String(input);
      requests.push(init === undefined ? { url } : { url, init });
      if (url.endsWith("/api/v1/agents") && init?.method === "POST") {
        return jsonResponse({
          id: "agent_1",
          configRevision: "revision_1",
        }, 201);
      }
      if (
        url.endsWith("/api/v1/agents/agent_1/runs") &&
        init?.method === "POST"
      ) {
        return jsonResponse({
          id: "run_1",
          agentId: "agent_1",
          configRevision: "revision_1",
          status: "AGENT_EXECUTION_STATUS_PENDING",
        }, 201);
      }
      if (
        url.endsWith("/api/v1/agents/agent_1/runs/run_1") &&
        init?.method === "GET"
      ) {
        runReads++;
        return jsonResponse({
          id: "run_1",
          agentId: "agent_1",
          configRevision: "revision_1",
          status: runReads === 1
            ? "AGENT_EXECUTION_STATUS_RUNNING"
            : "AGENT_EXECUTION_STATUS_SUCCEEDED",
          output: runReads === 1 ? undefined : { text: "fixed" },
        });
      }
      throw new Error(`unexpected request ${init?.method} ${url}`);
    };

    const agent = await Agent(
      {
        providerName: "managed",
        model: "gpt-5.5",
        idempotencyKey: "create-1",
      },
      {
        baseUrl: "https://gestalt.test",
        token: "secret",
        fetch,
        pollIntervalMs: 0,
      },
    );
    const run = await agent.sendMessage("Fix the test", {
      idempotencyKey: "run-1",
    });
    const result = await run.result;

    expect(agent.id).toBe("agent_1");
    expect(agent.configRevision).toBe("revision_1");
    expect(result.text).toBe("fixed");
    expect(result.turn.status).toBe("SUCCEEDED");
    expect(requests[0]?.init?.headers).toMatchObject({
      Authorization: "Bearer secret",
      "Idempotency-Key": "create-1",
    });
    expect(JSON.parse(String(requests[1]?.init?.body))).toMatchObject({
      message: "Fix the test",
    });
  });

  test("resumes by id and replays events by cursor", async () => {
    const requests: string[] = [];
    const fetch = async (
      input: string | URL | Request,
      init?: RequestInit,
    ): Promise<Response> => {
      const url = String(input);
      requests.push(url);
      if (url.endsWith("/api/v1/agents/agent_1")) {
        return jsonResponse({
          id: "agent_1",
          configRevision: "revision_1",
        });
      }
      if (url.endsWith("/api/v1/agents/agent_1/runs/run_1")) {
        return jsonResponse({
          id: "run_1",
          agentId: "agent_1",
          configRevision: "revision_1",
          status: "AGENT_EXECUTION_STATUS_RUNNING",
        });
      }
      if (url.endsWith("/api/v1/agents/agent_1/runs/run_1/events")) {
        return jsonResponse({
          events: [{
            id: "event_1",
            cursor: "cursor_1",
            sequence: "1",
            agentId: "agent_1",
            runId: "run_1",
            type: "AGENT_RUN_EVENT_TYPE_TEXT_DELTA",
            display: { text: "hello" },
          }],
        });
      }
      if (
        url.endsWith(
          "/api/v1/agents/agent_1/runs/run_1/events?after=cursor_1",
        )
      ) {
        return jsonResponse({
          events: [{
            id: "event_2",
            cursor: "cursor_2",
            sequence: "2",
            agentId: "agent_1",
            runId: "run_1",
            type: "AGENT_RUN_EVENT_TYPE_TURN_COMPLETED",
          }],
        });
      }
      throw new Error(`unexpected request ${init?.method} ${url}`);
    };

    const agent = await Agent(
      { id: "agent_1" },
      { baseUrl: "https://gestalt.test", fetch },
    );
    const run = await agent.getRun("run_1");
    const events: AgentTurnEvent[] = [];
    for await (const event of run) {
      events.push(event);
    }

    expect(events.map((event) => event.type)).toEqual([
      "text_delta",
      "turn_completed",
    ]);
    expect(requests).toContain(
      "https://gestalt.test/api/v1/agents/agent_1/runs/run_1/events?after=cursor_1",
    );
  });

  test("lists and resolves pending approval interactions", async () => {
    const bodies: unknown[] = [];
    const fetch = async (
      input: string | URL | Request,
      init?: RequestInit,
    ): Promise<Response> => {
      const url = String(input);
      if (url.endsWith("/api/v1/agents/agent_1")) {
        return jsonResponse({
          id: "agent_1",
          configRevision: "revision_1",
        });
      }
      if (url.endsWith("/api/v1/agents/agent_1/runs/run_1")) {
        return jsonResponse({
          id: "run_1",
          agentId: "agent_1",
          configRevision: "revision_1",
          status: "AGENT_EXECUTION_STATUS_WAITING_FOR_INPUT",
        });
      }
      if (url.endsWith("/interactions?state=pending")) {
        return jsonResponse({
          interactions: [{
            id: "interaction_1",
            agentId: "agent_1",
            runId: "run_1",
            kind: "AGENT_INTERACTION_KIND_APPROVAL",
            state: "AGENT_INTERACTION_STATE_PENDING",
            approval: { action: "run tests" },
          }],
        });
      }
      if (url.endsWith("/interactions/interaction_1/resolve")) {
        bodies.push(JSON.parse(String(init?.body)));
        return jsonResponse({
          id: "interaction_1",
          agentId: "agent_1",
          runId: "run_1",
          kind: "AGENT_INTERACTION_KIND_APPROVAL",
          state: "AGENT_INTERACTION_STATE_RESOLVED",
          approval: { action: "run tests" },
        });
      }
      throw new Error(`unexpected request ${init?.method} ${url}`);
    };

    const agent = await Agent(
      { id: "agent_1" },
      { baseUrl: "https://gestalt.test", fetch },
    );
    const run = await agent.getRun("run_1");
    const pending = await run.listPendingInteractions();
    await run.respond("interaction_1", { decision: "approve" });

    expect(pending).toHaveLength(1);
    expect(pending[0]).toMatchObject({
      kind: "approval",
      action: "run tests",
    });
    expect(bodies[0]).toEqual({
      resolution: {
        approval: {
          decision: "AGENT_APPROVAL_DECISION_APPROVE",
        },
      },
    });
  });
});

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
