/**
 * Request-scoped host-service transport for {@link Request.gestalt}.
 */

import type { RequestContext } from "./app.ts";
import {
  boundGestaltFromTransport,
  type BoundGestalt,
  type BoundHostService,
  type RequestGestaltOptions,
} from "./bound-gestalt.ts";
import {
  createHostServiceGrpcTransport,
  hostServiceMetadataInterceptors,
  parseHostServiceTarget,
  requireHostServiceTarget,
  type HostServiceGrpcTransport,
} from "./host-service.ts";

const BOUND_HOST_SERVICES: readonly BoundHostService[] = [
  "app",
  "identity",
  "agent",
  "workflow",
];

const requestGestaltScopeKey = Symbol("gestalt.requestGestaltScope");

interface GestaltRequest {
  __requestContext?: RequestContext | undefined;
  gestalt(options?: RequestGestaltOptions): Promise<BoundGestalt>;
}

interface ScopedGestaltOptions extends RequestGestaltOptions {
  context?: RequestContext | undefined;
}

class RequestGestaltScope {
  private transport: HostServiceGrpcTransport | undefined;
  private closed = false;

  constructor(private readonly capability: string) {}

  async gestalt(options?: ScopedGestaltOptions): Promise<BoundGestalt> {
    const capability = this.capability.trim();
    if (!capability) {
      throw new Error("gestalt: invocation capability is required");
    }
    if (this.closed) {
      throw new Error("gestalt: request scope is closed");
    }
    const { target } = requireHostServiceTarget("gestalt", {
      anyOf: options?.service ? [options.service] : BOUND_HOST_SERVICES,
    });
    if (!this.transport) {
      this.transport = createHostServiceGrpcTransport(
        parseHostServiceTarget("gestalt", target),
        hostServiceMetadataInterceptors(capability, ""),
      );
    }
    return boundGestaltFromTransport(this.transport, {
      context: options?.context,
      timeoutMs: options?.timeoutMs,
    });
  }

  close(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.transport?.close();
    this.transport = undefined;
  }
}

export function attachRequestGestaltScope(
  request: GestaltRequest,
  capability: string,
): void {
  const scope = new RequestGestaltScope(capability);
  Object.defineProperty(request, requestGestaltScopeKey, {
    value: scope,
    enumerable: false,
    configurable: true,
  });
  request.gestalt = (options?: RequestGestaltOptions) =>
    scope.gestalt({
      context: request.__requestContext,
      timeoutMs: options?.timeoutMs,
      service: options?.service,
    });
}

export function closeRequestGestaltScope(request: GestaltRequest): void {
  const scope = (request as GestaltRequest & {
    [requestGestaltScopeKey]?: RequestGestaltScope;
  })[requestGestaltScopeKey];
  scope?.close();
}
