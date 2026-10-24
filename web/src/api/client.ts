import createClient from "openapi-fetch";

import type { components, paths } from "./schema";

/** Typed client for the platform API, generated from academy/api/openapi.yaml (`pnpm gen`). Same origin.
 * `fetch` is looked up per call, not captured at import, so test interceptors (MSW) see the requests. */
export const api = createClient<paths>({
  baseUrl: location.origin,
  fetch: (request) => globalThis.fetch(request),
});

export type Readiness = components["schemas"]["Readiness"];
export type Problem = components["schemas"]["Problem"];

/** Query keys, one place, so SSE events can invalidate them later. */
export const keys = {
  readiness: ["readyz"] as const,
};
