import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";
import type { ReactNode } from "react";

import { makeRouter } from "@/app/router";

function testClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

/** Renders `ui` with a fresh query cache that does not retry (tests see failures at once). */
export function renderWithQuery(ui: ReactNode) {
  return render(<QueryClientProvider client={testClient()}>{ui}</QueryClientProvider>);
}

/** Renders the whole app (every route) at `path`, against the MSW platform. */
export function renderRoute(path: string) {
  const client = testClient();
  const router = makeRouter(client, createMemoryHistory({ initialEntries: [path] }));
  const view = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...view, router };
}
