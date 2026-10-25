import { QueryClient } from "@tanstack/react-query";
import { createRouter, type RouterHistory } from "@tanstack/react-router";

import { routeTree } from "@/routeTree.gen";

export interface RouterContext {
  queryClient: QueryClient;
}

export function makeRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    ...(history ? { history } : {}),
    context: { queryClient },
    defaultPreload: "intent",
    // Loaders read through the Query cache, which owns freshness
    defaultPreloadStaleTime: 0,
    scrollRestoration: true,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof makeRouter>;
  }
}
