import { createGestaltClient, session } from "../src/client/index.ts";
import { rest } from "../src/client/rest.ts";

export async function boot() {
  return createGestaltClient({
    transport: rest(),
    auth: session(),
    fetch,
  });
}
