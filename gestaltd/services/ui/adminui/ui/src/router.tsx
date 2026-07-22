import { base } from "@valon-technologies/gestalt-web/mount";
import { createRouter } from "@tanstack/react-router";
import { makeQueryClient } from "@/lib/query-client";
import { routeTree } from "./routeTree.gen";

export const queryClient = makeQueryClient();

export const router = createRouter({
  routeTree,
  context: { queryClient },
  basepath: base(),
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
