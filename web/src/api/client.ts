import createClient, { type Middleware } from "openapi-fetch";

import type { components, paths } from "./schema";

/** Typed client for the platform API, generated from academy/api/openapi.yaml (`pnpm gen`). Same origin.
 * `fetch` is looked up per call, not captured at import, so test interceptors (MSW) see the requests. */
export const api = createClient<paths>({
  baseUrl: location.origin,
  fetch: (request) => globalThis.fetch(request),
});

type Schemas = components["schemas"];
export type Readiness = Schemas["Readiness"];
export type Problem = Schemas["Problem"];
export type Me = Schemas["Me"];
export type CourseSummary = Schemas["CourseSummary"];
export type Course = Schemas["Course"];
export type ItemRef = Schemas["ItemRef"];
export type Item = Schemas["Item"];
export type ItemType = Schemas["ItemType"];

/** A problem document the API answered, as an Error (queries reject with it). */
export class ProblemError extends Error {
  constructor(readonly problem: Problem) {
    super(problem.detail ?? problem.title);
  }

  get status() {
    return this.problem.status;
  }
}

/** The page's session: GET /me, or POST /sessions when there is none (architecture.md "Security"). The cookie is
 * HttpOnly; what the page keeps is the CSRF token it sends as X-CSRF-Token on every change. */
let session: Promise<string> | undefined;

async function openSession(): Promise<string> {
  const me = await globalThis.fetch(`${location.origin}/api/v1/me`);
  const res = me.status === 401 ? await globalThis.fetch(`${location.origin}/api/v1/sessions`, { method: "POST" }) : me;
  if (!res.ok) throw new Error(`cannot sign in: ${String(res.status)}`);
  return ((await res.json()) as Me).csrf;
}

export function ensureSession(): Promise<string> {
  session ??= openSession().catch((err: unknown) => {
    session = undefined; // the next call tries again
    throw err;
  });
  return session;
}

/** Tests start each case signed out. */
export function forgetSession() {
  session = undefined;
}

const changes = new Set(["POST", "PUT", "PATCH", "DELETE"]);

const sessionMiddleware: Middleware = {
  async onRequest({ request, schemaPath }) {
    if (!schemaPath.startsWith("/api/") || schemaPath === "/api/v1/sessions") return request;
    const csrf = await ensureSession();
    if (changes.has(request.method)) request.headers.set("X-CSRF-Token", csrf);
    return request;
  },
  async onResponse({ request, response, schemaPath }) {
    // An expired session: sign in again and repeat a read once (a change is left to the caller)
    if (response.status !== 401 || !schemaPath.startsWith("/api/") || request.method !== "GET") return response;
    session = undefined;
    await ensureSession();
    return globalThis.fetch(request);
  },
};
api.use(sessionMiddleware);

/** Query keys, one place, so SSE events can invalidate them later. */
export const keys = {
  readiness: ["readyz"] as const,
  courses: ["courses"] as const,
  course: (course: string) => ["courses", course] as const,
  item: (course: string, item: string) => ["courses", course, "items", item] as const,
};

function fail(error: unknown): never {
  throw error instanceof Error ? error : new ProblemError(error as Problem);
}

export const queries = {
  courses: () => ({
    queryKey: keys.courses,
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/courses");
      return data?.courses ?? fail(error);
    },
  }),
  course: (course: string) => ({
    queryKey: keys.course(course),
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/courses/{course}", { params: { path: { course } } });
      return data ?? fail(error);
    },
  }),
  item: (course: string, item: string) => ({
    queryKey: keys.item(course, item),
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/courses/{course}/items/{item}", {
        params: { path: { course, item } },
      });
      return data ?? fail(error);
    },
  }),
};

/** Whether a query failed because the thing is not in the catalogue (404). */
export function isNotFound(error: unknown) {
  return error instanceof ProblemError && error.status === 404;
}
