/**
 * Bound provider Gestalt client over the host-service gRPC relay.
 */

import type { Transport } from "@connectrpc/connect";
import { Agent } from "./agent.ts";
import { App, type RequestContext } from "./app.ts";
import { Identity } from "./identity.ts";
import { Workflow } from "./workflow.ts";

export type BoundHostService = "app" | "identity" | "agent" | "workflow";

export interface BoundGestalt {
  readonly app: App;
  readonly identity: Identity;
  readonly agent: Agent;
  readonly workflows: Workflow;
}

export interface RequestGestaltOptions {
  timeoutMs?: number | undefined;
  service?: BoundHostService | undefined;
}

interface BoundGestaltFromTransportOptions extends RequestGestaltOptions {
  context?: RequestContext | undefined;
}

export function boundGestaltFromTransport(
  transport: Transport,
  options: BoundGestaltFromTransportOptions = {},
): BoundGestalt {
  const clientOptions = {
    context: options.context,
    timeoutMs: options.timeoutMs,
  };
  return {
    app: new App(transport, clientOptions),
    identity: new Identity(transport, clientOptions),
    agent: new Agent(transport, clientOptions),
    workflows: new Workflow(transport, clientOptions),
  };
}
