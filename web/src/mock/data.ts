// Design-time data for the screens whose backends do not exist yet (rewards, labstate, practice, streaks, quiz and
// lab runtime). Shapes follow what the API will serve; each screen reads it through `src/mock/index.ts`, so hooking a
// screen up means replacing one import with a query. The courses are the syllabus (docs/courses/kubernetes-syllabus.md).

import type { ItemType } from "@/api/client";

export type StationState = "passed" | "current" | "started" | "open" | "locked";
export type Difficulty = "easy" | "medium" | "hard";

export interface Station {
  id: string;
  title: string;
  /** What the section teaches, as AlgoMonster names patterns */
  topics: string[];
  items: { type: ItemType; title: string; minutes: number; done: boolean }[];
  state: StationState;
}

export interface Line {
  course: string;
  title: string;
  level: "beginner" | "intermediate" | "advanced";
  /** Courses to finish first ("After ...") */
  after: string[];
  layout?: string;
  stations: Station[];
}

function items(spec: string, done = 0): Station["items"] {
  // "L:From containers to orchestration:8|Q:Architecture check:5" → items; the first `done` are complete
  const kinds = { L: "lecture", Q: "quiz", B: "lab", C: "challenge" } as const;
  return spec.split("|").map((part, i) => {
    const [k = "L", title = "", minutes = "10"] = part.split(":");
    return { type: kinds[k as keyof typeof kinds], title, minutes: Number(minutes), done: i < done };
  });
}

function station(
  id: string,
  title: string,
  topics: string[],
  spec: string,
  state: StationState = "open",
  done?: number,
): Station {
  const list = items(spec, done ?? (state === "passed" ? 99 : 0));
  return { id, title, topics, items: list, state };
}

export const lines: Line[] = [
  {
    course: "cloud-networking",
    title: "Cloud networking basics",
    level: "beginner",
    after: [],
    stations: [
      station(
        "regions",
        "Regions and networks",
        ["Latency", "Private networks", "Gateways"],
        "L:Where your machines are:8|B:Regions and latency:15|L:Who can reach whom:10|B:Private networks:15|Q:Networks check:5",
        "passed",
      ),
      station(
        "connecting",
        "Connecting sites",
        ["Tunnels", "Routing", "Failover"],
        "L:Tunnels between regions:10|L:Routes that move:8|B:Routing around failure:20|Q:Routing check:5",
        "passed",
      ),
    ],
  },
  {
    course: "k8s-fundamentals",
    title: "Kubernetes Fundamentals",
    level: "beginner",
    after: [],
    stations: [
      station(
        "cluster",
        "Kubernetes and your cluster",
        ["Control plane", "Nodes", "Regions as labels"],
        "L:From containers to orchestration:8|L:The control plane and the nodes:10|Q:Architecture check:5|B:Meet your cluster:15",
        "passed",
      ),
      station(
        "kubectl",
        "Talking to the API with kubectl",
        ["get / describe", "Output formats", "Contexts"],
        "L:Every command is an API call:10|L:Reading objects:12|B:Find it with kubectl:15|Q:kubectl check:5|C:Select with labels:15|L:Explain and docs:8",
        "passed",
      ),
      station(
        "pods",
        "Pods",
        ["Pod lifecycle", "Restart policies", "Logs and exec"],
        "L:What a pod is:8|L:Pod lifecycle:12|Q:Pods check:5|B:Debug a crashing pod:15|C:First pod:10|L:Init and sidecar containers:10|B:Exec into a pod:10",
        "current",
        3,
      ),
      station(
        "deployments",
        "Deployments and ReplicaSets",
        ["Rollouts", "Rollbacks", "Scaling"],
        "L:Why pods need an owner:8|L:Rolling updates:12|B:Roll out and roll back:20|C:Zero-downtime rollout:20|Q:Deployments check:5",
      ),
      station(
        "services",
        "Services and DNS",
        ["ClusterIP", "NodePort", "Service DNS"],
        "L:A stable name for moving pods:10|L:Endpoints and kube-proxy:12|B:Expose a deployment:15|Q:Services check:5|C:Wire two services:15",
      ),
      station(
        "namespaces",
        "Namespaces and the shape of a cluster",
        ["Namespaces", "Labels", "Final exam"],
        "L:Namespaces:8|B:Tidy a cluster:15|Q:Final exam:30",
      ),
    ],
  },
  {
    course: "config-workloads",
    title: "Configuration and Workloads",
    level: "intermediate",
    after: ["Kubernetes Fundamentals"],
    stations: [
      station(
        "config",
        "ConfigMaps and Secrets",
        ["Env vs files", "Rotation"],
        "L:Config outside the image:10|B:Mount a ConfigMap:15|Q:Config check:5",
        "locked",
      ),
      station(
        "resources",
        "Resources",
        ["Requests", "Limits", "OOMKilled"],
        "L:Requests and limits:12|B:An OOMKilled pod:15|C:Right-size a deployment:20",
        "locked",
      ),
      station(
        "probes",
        "Health probes",
        ["Liveness", "Readiness", "Startup"],
        "L:Three probes:12|B:A pod that lies:15|Q:Probes check:5",
        "locked",
      ),
      station(
        "jobs",
        "Jobs and CronJobs",
        ["Completions", "Backoff", "Schedules"],
        "L:Work that ends:10|C:A nightly job:15",
        "locked",
      ),
      station(
        "storage",
        "Storage and StatefulSets",
        ["PVCs", "Stable identity"],
        "L:Volumes and claims:12|B:Data that survives:20|Q:Storage check:5",
        "locked",
      ),
      station(
        "daemonsets",
        "DaemonSets and the node",
        ["One per node", "Tolerations"],
        "L:One pod per node:10|B:A node agent:15|Q:Final exam:30",
        "locked",
      ),
    ],
  },
  {
    course: "across-regions",
    title: "Networking and Scheduling across Regions",
    level: "intermediate",
    after: ["Kubernetes Fundamentals", "Cloud networking basics"],
    stations: [
      station(
        "ingress",
        "Ingress",
        ["Per-site ingress", "Host rules"],
        "L:One door per site:10|B:Route by host:15",
        "locked",
      ),
      station(
        "policies",
        "Network policies",
        ["Default deny", "Namespaces"],
        "L:Who may talk to whom:12|C:Default deny:20",
        "locked",
      ),
      station(
        "scheduling",
        "Scheduling",
        ["Affinity", "Taints"],
        "L:How a pod finds a node:12|B:Pin to a region:15|Q:Scheduling check:5",
        "locked",
      ),
      station(
        "spreading",
        "Spreading over failure domains",
        ["Topology spread", "Zones as regions"],
        "L:Spread constraints:12|B:Scale across regions:20",
        "locked",
      ),
      station(
        "failure",
        "When a region fails",
        ["Node lease", "Eviction", "Recovery"],
        "L:What the cluster notices:12|B:Losing a site:25|Q:Failure check:5",
        "locked",
      ),
      station(
        "local",
        "Traffic that stays local, and global names",
        ["Topology-aware routing", "Geo DNS"],
        "L:Keep traffic in the region:10|B:Geo DNS failover:20|Q:Final exam:30",
        "locked",
      ),
    ],
  },
  {
    course: "production",
    title: "Production Kubernetes",
    level: "advanced",
    after: ["Configuration and Workloads", "Networking and Scheduling across Regions"],
    layout: "multi-cluster for section 8",
    stations: [
      station(
        "rbac",
        "Access control",
        ["RBAC", "Service accounts"],
        "L:Who can do what:12|C:Least privilege:20",
        "locked",
      ),
      station(
        "podsec",
        "Pod security",
        ["Security contexts", "Admission"],
        "L:Pod Security Standards:12|B:Harden a pod:15",
        "locked",
      ),
      station("autoscaling", "Autoscaling", ["HPA", "Metrics"], "L:Scale on load:12|B:Autoscale web:20", "locked"),
      station(
        "packaging",
        "Packaging: Helm and Kustomize",
        ["Overlays", "Charts"],
        "L:Overlays:10|C:Per-region overlay:20",
        "locked",
      ),
      station(
        "gitops",
        "GitOps",
        ["Flux", "Drift"],
        "L:The repo is the truth:10|B:Roll out with a commit:20",
        "locked",
      ),
      station(
        "observability",
        "Observability",
        ["Metrics", "Dashboards"],
        "L:What to watch:10|B:Find the slow region:20",
        "locked",
      ),
      station(
        "troubleshooting",
        "Troubleshooting (capstone)",
        ["Method", "Everything at once"],
        "B:Five things are broken:45|Q:Final exam:30",
        "locked",
      ),
      station(
        "multicluster",
        "Multi-cluster",
        ["Skupper", "Cluster failover"],
        "L:One cluster per region:12|B:Lose a cluster:25",
        "locked",
      ),
    ],
  },
];

export interface Problem {
  id: string;
  title: string;
  type: "lab" | "challenge" | "quiz";
  course: string;
  section: string;
  topics: string[];
  difficulty: Difficulty;
  minutes: number;
  xp: number;
  status: "solved" | "attempted" | "todo";
  /** Best result: a lab's time, a quiz's score, a challenge's time to Ready */
  best?: string;
  /** Not runnable on the running layout */
  layout?: string;
  /** Where the player opens it */
  to: { course: string; item: string };
}

const demo = { course: "k8s-intro", item: "meet-the-cluster" };

export const problems: Problem[] = [
  {
    id: "regions-and-latency",
    title: "Regions and latency",
    type: "lab",
    course: "Cloud networking basics",
    section: "Regions and networks",
    topics: ["Networking"],
    difficulty: "easy",
    minutes: 15,
    xp: 100,
    status: "solved",
    best: "9 min",
    to: demo,
  },
  {
    id: "private-networks",
    title: "Private networks",
    type: "lab",
    course: "Cloud networking basics",
    section: "Regions and networks",
    topics: ["Networking", "Security"],
    difficulty: "easy",
    minutes: 15,
    xp: 100,
    status: "solved",
    best: "12 min",
    to: demo,
  },
  {
    id: "routing-around-failure",
    title: "Routing around failure",
    type: "lab",
    course: "Cloud networking basics",
    section: "Connecting sites",
    topics: ["Networking", "Failure"],
    difficulty: "medium",
    minutes: 20,
    xp: 150,
    status: "solved",
    best: "17 min",
    to: demo,
  },
  {
    id: "meet-the-cluster",
    title: "Meet your cluster",
    type: "lab",
    course: "Kubernetes Fundamentals",
    section: "Kubernetes and your cluster",
    topics: ["Cluster"],
    difficulty: "easy",
    minutes: 15,
    xp: 100,
    status: "solved",
    best: "6 min",
    to: demo,
  },
  {
    id: "architecture-check",
    title: "Architecture check",
    type: "quiz",
    course: "Kubernetes Fundamentals",
    section: "Kubernetes and your cluster",
    topics: ["Cluster"],
    difficulty: "easy",
    minutes: 5,
    xp: 30,
    status: "solved",
    best: "7 of 8",
    to: { course: "k8s-intro", item: "pods-check" },
  },
  {
    id: "find-it-with-kubectl",
    title: "Find it with kubectl",
    type: "lab",
    course: "Kubernetes Fundamentals",
    section: "Talking to the API",
    topics: ["kubectl"],
    difficulty: "easy",
    minutes: 15,
    xp: 100,
    status: "solved",
    best: "11 min",
    to: demo,
  },
  {
    id: "select-with-labels",
    title: "Select with labels",
    type: "challenge",
    course: "Kubernetes Fundamentals",
    section: "Talking to the API",
    topics: ["kubectl", "Labels"],
    difficulty: "easy",
    minutes: 15,
    xp: 80,
    status: "solved",
    best: "4 s to Ready",
    to: { course: "k8s-intro", item: "first-pod" },
  },
  {
    id: "pods-check",
    title: "Pods check",
    type: "quiz",
    course: "Kubernetes Fundamentals",
    section: "Pods",
    topics: ["Pods"],
    difficulty: "easy",
    minutes: 5,
    xp: 30,
    status: "attempted",
    best: "5 of 8",
    to: { course: "k8s-intro", item: "pods-check" },
  },
  {
    id: "debug-a-crashing-pod",
    title: "Debug a crashing pod",
    type: "lab",
    course: "Kubernetes Fundamentals",
    section: "Pods",
    topics: ["Pods", "Debugging"],
    difficulty: "medium",
    minutes: 15,
    xp: 150,
    status: "attempted",
    to: demo,
  },
  {
    id: "first-pod",
    title: "First pod",
    type: "challenge",
    course: "Kubernetes Fundamentals",
    section: "Pods",
    topics: ["Pods", "Manifests"],
    difficulty: "easy",
    minutes: 10,
    xp: 80,
    status: "todo",
    to: { course: "k8s-intro", item: "first-pod" },
  },
  {
    id: "exec-into-a-pod",
    title: "Exec into a pod",
    type: "lab",
    course: "Kubernetes Fundamentals",
    section: "Pods",
    topics: ["Pods", "Debugging"],
    difficulty: "easy",
    minutes: 10,
    xp: 100,
    status: "todo",
    to: demo,
  },
  {
    id: "roll-out-and-roll-back",
    title: "Roll out and roll back",
    type: "lab",
    course: "Kubernetes Fundamentals",
    section: "Deployments",
    topics: ["Deployments"],
    difficulty: "medium",
    minutes: 20,
    xp: 150,
    status: "todo",
    to: demo,
  },
  {
    id: "zero-downtime-rollout",
    title: "Zero-downtime rollout",
    type: "challenge",
    course: "Kubernetes Fundamentals",
    section: "Deployments",
    topics: ["Deployments", "Probes"],
    difficulty: "medium",
    minutes: 20,
    xp: 150,
    status: "todo",
    to: { course: "k8s-intro", item: "first-pod" },
  },
  {
    id: "expose-a-deployment",
    title: "Expose a deployment",
    type: "lab",
    course: "Kubernetes Fundamentals",
    section: "Services and DNS",
    topics: ["Services"],
    difficulty: "easy",
    minutes: 15,
    xp: 100,
    status: "todo",
    to: demo,
  },
  {
    id: "wire-two-services",
    title: "Wire two services",
    type: "challenge",
    course: "Kubernetes Fundamentals",
    section: "Services and DNS",
    topics: ["Services", "DNS"],
    difficulty: "medium",
    minutes: 15,
    xp: 150,
    status: "todo",
    to: { course: "k8s-intro", item: "first-pod" },
  },
  {
    id: "k8s-final",
    title: "Kubernetes Fundamentals final exam",
    type: "quiz",
    course: "Kubernetes Fundamentals",
    section: "Namespaces",
    topics: ["Exam"],
    difficulty: "medium",
    minutes: 30,
    xp: 200,
    status: "todo",
    to: { course: "k8s-intro", item: "pods-check" },
  },
  {
    id: "an-oomkilled-pod",
    title: "An OOMKilled pod",
    type: "lab",
    course: "Configuration and Workloads",
    section: "Resources",
    topics: ["Resources", "Debugging"],
    difficulty: "medium",
    minutes: 15,
    xp: 150,
    status: "todo",
    to: demo,
  },
  {
    id: "right-size-a-deployment",
    title: "Right-size a deployment",
    type: "challenge",
    course: "Configuration and Workloads",
    section: "Resources",
    topics: ["Resources"],
    difficulty: "medium",
    minutes: 20,
    xp: 150,
    status: "todo",
    to: { course: "k8s-intro", item: "first-pod" },
  },
  {
    id: "a-pod-that-lies",
    title: "A pod that lies",
    type: "lab",
    course: "Configuration and Workloads",
    section: "Health probes",
    topics: ["Probes", "Debugging"],
    difficulty: "medium",
    minutes: 15,
    xp: 150,
    status: "todo",
    to: demo,
  },
  {
    id: "a-nightly-job",
    title: "A nightly job",
    type: "challenge",
    course: "Configuration and Workloads",
    section: "Jobs and CronJobs",
    topics: ["Jobs"],
    difficulty: "easy",
    minutes: 15,
    xp: 80,
    status: "todo",
    to: { course: "k8s-intro", item: "first-pod" },
  },
  {
    id: "data-that-survives",
    title: "Data that survives",
    type: "lab",
    course: "Configuration and Workloads",
    section: "Storage",
    topics: ["Storage"],
    difficulty: "hard",
    minutes: 20,
    xp: 250,
    status: "todo",
    to: demo,
  },
  {
    id: "default-deny",
    title: "Default deny",
    type: "challenge",
    course: "Networking and Scheduling across Regions",
    section: "Network policies",
    topics: ["Networking", "Security"],
    difficulty: "hard",
    minutes: 20,
    xp: 250,
    status: "todo",
    to: { course: "k8s-intro", item: "first-pod" },
  },
  {
    id: "scale-across-regions",
    title: "Scale across regions",
    type: "lab",
    course: "Networking and Scheduling across Regions",
    section: "Spreading",
    topics: ["Scheduling", "Regions"],
    difficulty: "medium",
    minutes: 20,
    xp: 150,
    status: "solved",
    best: "14 min",
    to: demo,
  },
  {
    id: "losing-a-site",
    title: "Losing a site",
    type: "lab",
    course: "Networking and Scheduling across Regions",
    section: "When a region fails",
    topics: ["Failure", "Regions"],
    difficulty: "hard",
    minutes: 25,
    xp: 250,
    status: "solved",
    best: "22 min",
    to: demo,
  },
  {
    id: "geo-dns-failover",
    title: "Geo DNS failover",
    type: "lab",
    course: "Networking and Scheduling across Regions",
    section: "Traffic that stays local",
    topics: ["DNS", "Failure"],
    difficulty: "hard",
    minutes: 20,
    xp: 250,
    status: "todo",
    to: demo,
  },
  {
    id: "five-things-are-broken",
    title: "Five things are broken",
    type: "lab",
    course: "Production Kubernetes",
    section: "Troubleshooting",
    topics: ["Debugging"],
    difficulty: "hard",
    minutes: 45,
    xp: 400,
    status: "todo",
    to: demo,
  },
  {
    id: "lose-a-cluster",
    title: "Lose a cluster",
    type: "lab",
    course: "Production Kubernetes",
    section: "Multi-cluster",
    topics: ["Failure", "Multi-cluster"],
    difficulty: "hard",
    minutes: 25,
    xp: 300,
    status: "todo",
    layout: "multi-cluster",
    to: demo,
  },
];

export const topics = Array.from(new Set(problems.flatMap((p) => p.topics))).sort();

export interface Badge {
  id: string;
  title: string;
  rule: string;
  earned?: string;
}

export const badges: Badge[] = [
  { id: "first-light", title: "First light", rule: "Pass your first lab", earned: "2026-10-02" },
  {
    id: "cartographer",
    title: "Cartographer",
    rule: "Pass every lab of Cloud networking basics",
    earned: "2026-10-09",
  },
  { id: "no-hints", title: "Unaided", rule: "Pass a lab without opening a hint", earned: "2026-10-11" },
  { id: "site-down", title: "Site down", rule: "Pass Losing a site", earned: "2026-10-19" },
  { id: "week", title: "A full week", rule: "Practise seven days in a row", earned: "2026-10-21" },
  { id: "fundamentals", title: "Fundamentals", rule: "Complete Kubernetes Fundamentals" },
  { id: "clean-exam", title: "Clean sheet", rule: "Score 100 % on a final exam" },
  { id: "hard-three", title: "Three hard ones", rule: "Pass three hard labs or challenges" },
  { id: "month", title: "A full month", rule: "Practise thirty days in a row" },
];

export interface Activity {
  at: string;
  kind:
    "lab.passed" | "quiz.submitted" | "challenge.accepted" | "lecture.completed" | "badge.awarded" | "lab.abandoned";
  title: string;
  detail: string;
}

export const activity: Activity[] = [
  {
    at: "2026-10-25T09:42:00Z",
    kind: "lecture.completed",
    title: "Pod lifecycle",
    detail: "Kubernetes Fundamentals 3.2",
  },
  { at: "2026-10-25T09:20:00Z", kind: "quiz.submitted", title: "Pods check", detail: "5 of 8, not passed yet" },
  {
    at: "2026-10-24T18:05:00Z",
    kind: "lab.abandoned",
    title: "Debug a crashing pod",
    detail: "Stopped at step 2 of 4",
  },
  {
    at: "2026-10-24T17:30:00Z",
    kind: "challenge.accepted",
    title: "Select with labels",
    detail: "+80 XP, Ready in 4 s",
  },
  { at: "2026-10-21T20:11:00Z", kind: "badge.awarded", title: "A full week", detail: "Seven days in a row" },
  { at: "2026-10-19T19:48:00Z", kind: "lab.passed", title: "Losing a site", detail: "+250 XP, 22 min, no hints" },
];

/** XP per level, a placeholder curve until `rewards` defines one: level n is reached at 250·(n-1)² XP */
export function levelFor(xp: number) {
  let level = 1;
  while (250 * level * level <= xp) level++;
  const floor = 250 * (level - 1) * (level - 1);
  const next = 250 * level * level;
  return { level, floor, next };
}

export const learner = {
  name: "learner",
  xp: 2_840,
  xpToday: 60,
  streak: { current: 12, best: 21, today: true },
  resume: {
    course: "Kubernetes Fundamentals",
    courseId: "k8s-intro",
    item: "what-is-a-pod",
    number: "3.3",
    title: "Pods check",
    type: "quiz" as ItemType,
    progress: 41,
  },
};

/** A deterministic year of practice: minutes per day, the last day today */
export function practiceDays(today = new Date(), days = 364): { date: string; minutes: number }[] {
  const out: { date: string; minutes: number }[] = [];
  let seed = 7;
  for (let i = days - 1; i >= 0; i--) {
    seed = (seed * 9301 + 49297) % 233280;
    const r = seed / 233280;
    const d = new Date(today);
    d.setDate(d.getDate() - i);
    const recent = i < 12; // the current streak
    const active = recent || (i > 90 && i < 260 ? r > 0.62 : r > 0.45);
    out.push({ date: d.toISOString().slice(0, 10), minutes: active ? Math.round(10 + r * 70) : 0 });
  }
  return out;
}

/** Labs and challenges passed, per difficulty (quizzes are scored, not solved) */
export function solvedByDifficulty() {
  const out = { easy: { done: 0, total: 0 }, medium: { done: 0, total: 0 }, hard: { done: 0, total: 0 } };
  for (const p of problems) {
    if (p.type === "quiz") continue;
    out[p.difficulty].total++;
    if (p.status === "solved") out[p.difficulty].done++;
  }
  return out;
}

/** Progress per topic, AlgoMonster's patterns: how many of a topic's labs and challenges are passed */
export function topicProgress() {
  return topics
    .map((t) => {
      const of = problems.filter((p) => p.type !== "quiz" && p.topics.includes(t));
      return { topic: t, done: of.filter((p) => p.status === "solved").length, total: of.length };
    })
    .filter((t) => t.total > 0)
    .sort((a, b) => b.done / b.total - a.done / a.total || b.total - a.total);
}
