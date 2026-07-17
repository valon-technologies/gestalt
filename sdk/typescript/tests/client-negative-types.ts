/**
 * Negative compile-time checks for the server-side client API.
 *
 * These files are typechecked but not executed.
 */

import { bearer, createGestaltClient, grpc, rest } from "@valon-technologies/gestalt/client";

// @ts-expect-error SessionAuth is not part of the server-side client API.
import { session } from "@valon-technologies/gestalt/client";

// @ts-expect-error address is required for external server clients.
void createGestaltClient({
  transport: rest(),
  auth: bearer(() => "token"),
});

void createGestaltClient({
  address: "https://valon.tools",
  transport: grpc(),
  auth: bearer(async () => "token"),
});

void session;
