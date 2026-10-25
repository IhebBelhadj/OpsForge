import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

import type { components } from "@/api/schema";

type Schemas = components["schemas"];

/** Spec-shaped fixtures (typed by the generated schema, so they break when the contract does). The course is the
 * golden demo pack's (academy/testdata/packs/demo), as the platform serves it. */
export const fixtures = {
  ready: {
    status: "ready",
    checks: [
      { name: "database", ok: true },
      { name: "toolchain", ok: true },
    ],
  } satisfies Schemas["Readiness"],
  toolchainDown: {
    status: "unavailable",
    checks: [
      { name: "database", ok: true },
      { name: "toolchain", ok: false, detail: "toolchain speaks contract 2, academy speaks 1" },
    ],
  } satisfies Schemas["Readiness"],
  me: { user: { id: 1, name: "learner" }, csrf: "csrf-token" } satisfies Schemas["Me"],
  course: {
    id: "k8s-intro",
    pack: "demo",
    title: "Kubernetes introduction",
    subtitle: "One of everything, for the compiler's golden test.",
    level: "beginner",
    minutes: 33,
    needs: ["k3s"],
    requires: [],
    layout: null,
    badge: "intro-done",
    items: 4,
    section_count: 1,
    item_counts: { lecture: 1, quiz: 1, lab: 1, challenge: 1 },
    description: "A **tiny** course.\n",
    outcomes: ["Read a pod's status", "Write a pod manifest"],
    requirements: ["Basic shell use"],
    content_hash: "ad7ef344f4bcb582",
    sections: [
      {
        title: "Pods",
        layout: null,
        needs: [],
        minutes: 33,
        items: [
          {
            id: "what-is-a-pod",
            type: "lecture",
            title: "What is a pod",
            minutes: 8,
            content_hash: "18e3d0e07448638c",
          },
          { id: "pods-check", type: "quiz", title: "Pods check", minutes: 5, content_hash: "61c63c2c869f2c32" },
          {
            id: "meet-the-cluster",
            type: "lab",
            title: "Meet the cluster",
            minutes: 10,
            content_hash: "4a96800a992f8941",
          },
          { id: "first-pod", type: "challenge", title: "First pod", minutes: 10, content_hash: "f8ca6f717436e90f" },
        ],
      },
    ],
  } satisfies Schemas["Course"],
  items: {
    "what-is-a-pod": {
      course: "k8s-intro",
      id: "what-is-a-pod",
      type: "lecture",
      title: "What is a pod",
      minutes: 8,
      section: 0,
      content_hash: "18e3d0e07448638c",
      lecture: {
        markdown:
          "A pod is one or more containers that share a network namespace.\n\n![A pod](../../../assets/pod.svg)\n\n> [!NOTE]\n> Pods on `${node:c}` run in site c.\n",
        resources: [{ title: "Kubernetes docs: Pods", url: "https://kubernetes.io/docs/concepts/workloads/pods/" }],
      },
    },
    "pods-check": {
      course: "k8s-intro",
      id: "pods-check",
      type: "quiz",
      title: "Pods check",
      minutes: 5,
      section: 0,
      content_hash: "61c63c2c869f2c32",
      quiz: { pass: 70, xp: 20, time_limit_s: null, questions: 2 },
    },
    "meet-the-cluster": {
      course: "k8s-intro",
      id: "meet-the-cluster",
      type: "lab",
      title: "Meet the cluster",
      minutes: 10,
      section: 0,
      content_hash: "4a96800a992f8941",
      lab: {
        id: "meet-the-cluster",
        summary: "List the nodes and find which site each one is in.",
        xp: 100,
        needs: ["k3s"],
        requires: [],
        layout: null,
        briefing: "The cluster spans three sites.\n",
        debrief: "",
        concepts: [],
        cluster_changes: [],
        namespace: true,
        files: ["hello.yaml"],
        steps: [
          {
            id: "nodes",
            title: "List the nodes",
            body: "```bash\nopsforge kubectl get nodes\n```\n",
            check: { type: "probe" },
            hints: 1,
          },
          {
            id: "apply",
            title: "Apply a manifest",
            body: "Apply `hello.yaml` in `${ns}`.",
            open: "hello.yaml",
            check: { type: "confirm", label: "Applied" },
            hints: 0,
          },
        ],
      },
    },
    "first-pod": {
      course: "k8s-intro",
      id: "first-pod",
      type: "challenge",
      title: "First pod",
      minutes: 10,
      section: 0,
      content_hash: "f8ca6f717436e90f",
      challenge: {
        difficulty: "easy",
        xp: 50,
        needs: ["k3s"],
        statement: "Run one pod named `web` from the image `traefik/whoami`.\n",
        files: ["pod.yaml"],
        starter: { "pod.yaml": "apiVersion: v1\nkind: Pod\nmetadata:\n  name: web\n" },
        time_limit_s: 60,
        tests: ["pod is ready"],
        hints: 1,
      },
    },
  } satisfies Record<string, Schemas["Item"]>,
};

export function notFound(detail: string): Schemas["Problem"] {
  return { type: "/problems/not-found", title: "Not found", status: 404, detail };
}

const problem = { headers: { "Content-Type": "application/problem+json" } };

/** The platform as the tests see it; a test overrides a handler with server.use(...). */
export const server = setupServer(
  http.get("*/healthz", () => HttpResponse.json({ status: "ok", version: "test" })),
  http.get("*/readyz", () => HttpResponse.json(fixtures.ready)),
  http.get("*/api/v1/me", () => HttpResponse.json(fixtures.me)),
  http.post("*/api/v1/sessions", () => HttpResponse.json(fixtures.me, { status: 201 })),
  http.get("*/api/v1/courses", () => {
    const {
      description: _d,
      outcomes: _o,
      requirements: _r,
      content_hash: _h,
      sections: _s,
      ...summary
    } = fixtures.course;
    return HttpResponse.json({ courses: [summary] });
  }),
  http.get("*/api/v1/courses/:course", ({ params }) =>
    params.course === fixtures.course.id
      ? HttpResponse.json(fixtures.course)
      : HttpResponse.json(notFound(`course ${String(params.course)} is not in the catalogue`), {
          status: 404,
          ...problem,
        }),
  ),
  http.get("*/api/v1/courses/:course/items/:item", ({ params }) => {
    const item = (fixtures.items as Record<string, Schemas["Item"]>)[String(params.item)];
    return params.course === fixtures.course.id && item
      ? HttpResponse.json(item)
      : HttpResponse.json(notFound(`item ${String(params.item)} is not in the catalogue`), { status: 404, ...problem });
  }),
);

export { http, HttpResponse };
