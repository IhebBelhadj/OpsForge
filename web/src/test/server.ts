import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

import type { components } from "@/api/schema";

type Readiness = components["schemas"]["Readiness"];

/** Spec-shaped fixtures (typed by the generated schema, so they break when the contract does). */
export const fixtures = {
  ready: {
    status: "ready",
    checks: [
      { name: "database", ok: true },
      { name: "toolchain", ok: true },
    ],
  },
  toolchainDown: {
    status: "unavailable",
    checks: [
      { name: "database", ok: true },
      { name: "toolchain", ok: false, detail: "toolchain speaks contract 2, academy speaks 1" },
    ],
  },
} satisfies Record<string, Readiness>;

/** The platform as the tests see it; a test overrides a handler with server.use(...). */
export const server = setupServer(
  http.get("*/healthz", () => HttpResponse.json({ status: "ok", version: "test" })),
  http.get("*/readyz", () => HttpResponse.json(fixtures.ready)),
);

export { http, HttpResponse };
