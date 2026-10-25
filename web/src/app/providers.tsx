import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useState } from "react";

import { ProblemError } from "@/api/client";

import { makeRouter } from "./router";

/** The app: one query cache, the router on top of it. */
export function App() {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          // A problem the API answered (404, 401) is the answer; only failures to answer are worth retrying
          queries: {
            staleTime: 10_000,
            retry: (n, err) => n < 3 && !(err instanceof ProblemError && err.status < 500),
          },
        },
      }),
  );
  const [router] = useState(() => makeRouter(queryClient));
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
