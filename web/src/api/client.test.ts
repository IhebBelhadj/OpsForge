import { describe, expect, it } from "vitest";

import { fixtures, http, HttpResponse, server } from "@/test/server";

import { api, isNotFound, queries } from "./client";

describe("the API client", () => {
  it("signs in when the page has no session, then reads", async () => {
    let signedIn = false;
    server.use(
      http.get("*/api/v1/me", () =>
        signedIn ? HttpResponse.json(fixtures.me) : HttpResponse.json({}, { status: 401 }),
      ),
      http.post("*/api/v1/sessions", () => {
        signedIn = true;
        return HttpResponse.json(fixtures.me, { status: 201 });
      }),
    );
    const courses = await queries.courses().queryFn();
    expect(signedIn).toBe(true);
    expect(courses.map((c) => c.id)).toEqual(["k8s-intro"]);
  });

  it("sends the session's CSRF token with a change, not with a read", async () => {
    const seen: (string | null)[] = [];
    server.use(
      http.post("*/api/v1/admin/import", ({ request }) => {
        seen.push(request.headers.get("X-CSRF-Token"));
        return HttpResponse.json({ changed: false, added: {}, updated: {}, retired: {}, unchanged: {} });
      }),
      http.get("*/api/v1/courses", ({ request }) => {
        seen.push(request.headers.get("X-CSRF-Token"));
        return HttpResponse.json({ courses: [] });
      }),
    );
    await api.POST("/api/v1/admin/import");
    await api.GET("/api/v1/courses");
    expect(seen).toEqual(["csrf-token", null]);
  });

  it("signs in again and repeats a read when the session expired", async () => {
    let expired = true;
    server.use(
      http.get("*/api/v1/courses", () =>
        expired ? HttpResponse.json({}, { status: 401 }) : HttpResponse.json({ courses: [] }),
      ),
      http.get("*/api/v1/me", () => {
        if (!expired) return HttpResponse.json(fixtures.me);
        expired = false; // the first /me (before the read) still finds the old session
        return HttpResponse.json(fixtures.me);
      }),
    );
    expect(await queries.courses().queryFn()).toEqual([]);
  });

  it("rejects with the problem, so screens can tell a 404", async () => {
    const err: unknown = await queries
      .course("nope")
      .queryFn()
      .catch((e: unknown) => e);
    expect(isNotFound(err)).toBe(true);
  });
});
