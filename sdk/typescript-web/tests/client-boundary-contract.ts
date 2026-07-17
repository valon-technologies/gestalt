import {
  bearer,
  createGestaltClient,
  session,
  unauthenticated,
  type ClientOptions,
} from "../src/index.ts";

// @ts-expect-error provider authoring must not be exported from gestalt-web.
import { defineApp } from "../src/index.ts";

// @ts-expect-error gestalt-web root must not export mount helpers.
import { base } from "../src/index.ts";

// @ts-expect-error browser clients do not accept transport selectors.
({ auth: session(), transport: { kind: "grpc" } } satisfies ClientOptions);

void bearer;
void createGestaltClient;
void unauthenticated;
void defineApp;
void base;
