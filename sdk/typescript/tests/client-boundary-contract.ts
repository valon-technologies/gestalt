import {
  bearer,
  createGestaltClient,
  grpc,
  rest,
  unauthenticated,
  type GrpcClientOptions,
  type RestClientOptions,
} from "@valon-technologies/gestalt/client";

// Stand-in for gestalt-web session auth; must not be imported from the web
// package here or core SDK typecheck would resolve web client runtime deps.
function session(): { kind: "session" } {
  return { kind: "session" };
}

const address = "https://gestalt.example.test";

// @ts-expect-error server clients require an explicit address.
({ transport: rest(), auth: bearer(() => "token") } satisfies RestClientOptions);

// @ts-expect-error server clients do not accept session auth.
({ address, transport: grpc(), auth: session() } satisfies GrpcClientOptions);

void bearer;
void createGestaltClient;
void grpc;
void rest;
void unauthenticated;
