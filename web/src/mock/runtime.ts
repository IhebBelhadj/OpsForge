// Design-time runtime of quiz attempts, lab sessions and challenge runs: what the server will send once those screens
// are hooked to quiz, labsession and challenge. Keys live here only because nothing is scored on a server yet.

export interface MockQuestion {
  id: string;
  kind: "single" | "multiple";
  prompt: string;
  /** Command output shown with the question (the "what is wrong here?" kind) */
  output?: string;
  choices: string[];
  answer: number[];
  explain: string;
  see?: string;
}

export const quizQuestions: MockQuestion[] = [
  {
    id: "q1",
    kind: "single",
    prompt: "What runs the containers of a pod on its node?",
    choices: ["The API server", "The kubelet, through the container runtime", "kube-proxy", "The scheduler"],
    answer: [1],
    explain: "The scheduler only picks the node; the kubelet on that node asks the container runtime to start them.",
    see: "1.2 The control plane and the nodes",
  },
  {
    id: "q2",
    kind: "single",
    prompt: "A container exits with code 1 a second after it starts, again and again. What does the pod show?",
    output: "NAME   READY   STATUS             RESTARTS      AGE\nweb    0/1     CrashLoopBackOff   4 (21s ago)   2m",
    choices: ["Running", "Pending", "CrashLoopBackOff", "ImagePullBackOff"],
    answer: [2],
    explain:
      "The kubelet restarts it with a growing delay (10 s, 20 s, 40 s… up to 5 min) and shows CrashLoopBackOff meanwhile.",
    see: "3.2 Pod lifecycle",
  },
  {
    id: "q3",
    kind: "multiple",
    prompt: "Which of these do the containers of one pod share? Select all that apply.",
    choices: [
      "The network namespace (one IP)",
      "The filesystem of their images",
      "Volumes the pod declares",
      "Their restart count",
    ],
    answer: [0, 2],
    explain:
      "Containers of a pod share its IP and the volumes it mounts; each keeps its own image filesystem and restarts.",
    see: "3.1 What a pod is",
  },
  {
    id: "q4",
    kind: "single",
    prompt: "This pod never leaves Pending. What is the most likely reason?",
    output:
      "Events:\n  Type     Reason            From               Message\n  ----     ------            ----               -------\n  Warning  FailedScheduling  default-scheduler  0/4 nodes are available:\n           4 Insufficient memory.",
    choices: [
      "The image does not exist",
      "No node has enough unrequested memory for its requests",
      "The container keeps crashing",
      "The namespace is missing",
    ],
    answer: [1],
    explain: "FailedScheduling with Insufficient memory: the requests are larger than what any node has left.",
    see: "3.2 Pod lifecycle",
  },
  {
    id: "q5",
    kind: "single",
    prompt: "Which restartPolicy fits a pod that runs a database migration once?",
    choices: ["Always", "OnFailure", "Never restart, delete it by hand", "It depends on the image"],
    answer: [1],
    explain:
      "OnFailure retries a failed run and leaves a successful one Completed; Always would rerun the migration forever.",
    see: "3.2 Pod lifecycle",
  },
];

export const terminalTranscript = `lab OpsForge $ opsforge kubectl -n lab-7 get pods
NAME                   READY   STATUS             RESTARTS      AGE
web-6c9f8d7b5d-x2kqp   0/1     CrashLoopBackOff   4 (18s ago)   2m11s
lab OpsForge $ opsforge kubectl -n lab-7 logs web-6c9f8d7b5d-x2kqp
error: unknown flag: --listen
usage: whoami [--port PORT] [--name NAME]
lab OpsForge $ `;

export const setupLog = [
  "Checking the lab: up, stretched layout",
  "Creating namespace lab-7 on cluster lab",
  "Applying web.yaml (Deployment web, 1 replica)",
  "Waiting for the first restart",
  "Ready",
];

export interface MockSite {
  id: string;
  name: string;
  x: number;
  y: number;
  nodes: string[];
}

export const sites: MockSite[] = [
  { id: "a", name: "Region a", x: 110, y: 70, nodes: ["a-s1", "a-w1"] },
  { id: "b", name: "Region b", x: 490, y: 70, nodes: ["b-w1"] },
  { id: "c", name: "Region c", x: 300, y: 250, nodes: ["c-w1"] },
];

export const links: { from: string; to: string; rtt: string; state: "nominal" | "changed" | "cut" }[] = [
  { from: "a", to: "b", rtt: "40 ms", state: "nominal" },
  { from: "a", to: "c", rtt: "80 ms", state: "changed" },
  { from: "b", to: "c", rtt: "60 ms", state: "nominal" },
];

export interface Submission {
  at: string;
  verdict: "Accepted" | "Wrong answer" | "Invalid manifest" | "Time limit exceeded";
  runtime?: string;
}

export const submissions: Submission[] = [
  { at: "2026-10-24T17:22:00Z", verdict: "Invalid manifest" },
  { at: "2026-10-24T17:26:00Z", verdict: "Wrong answer", runtime: "6 s" },
];
