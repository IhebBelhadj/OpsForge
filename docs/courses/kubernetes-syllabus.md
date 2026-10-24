# Kubernetes syllabus: basics to advanced

Four courses, taken in order. Each section has lectures (📖), a quiz (❓), usually a lab (⌨) and often a coding
challenge (💻, LeetCode-style: write manifests in the editor, Run = server dry-run, Submit = applied in a scratch
namespace and tested; difficulty Easy/Medium/Hard). Lab rows say what the learner does, what the check reads on the
live lab, and what the lab needs. Labs that involve writing YAML open their files in the Editor tab. **Existing** marks the six Phase 10
labs; the rest are new. Every lab runs in its own namespace `lab-<id>` unless it says otherwise.

What the lab offers (checked 2026-10-24, stretched layout): K3s v1.37 with a server in a and agents in b and c,
Traefik on every site (hostPorts 80/443 behind each gateway), CoreDNS, local-path storage (default StorageClass),
Gateway API CRDs, K3s's embedded network-policy controller, the zot image cache, Helm 3 on the host, kubectl on the
host (`opsforge kubectl`), Flux with the ci node, VictoriaMetrics + Grafana with the mon node, Skupper in the
multi-cluster layout. Not available yet: metrics-server (disabled; spike S2), a CNI other than flannel.

Durations are reading/doing time estimates; they are reviewed after the live run of each lab.

---

## Course 1: Kubernetes Fundamentals (beginner)

*Run, expose and debug workloads on a cluster spread over three regions.* Needs: `k3s`, stretched layout.
Outcomes: read the cluster with kubectl; write and apply manifests; run, update and roll back Deployments; expose
them with Services; debug the common pod failures.

### 1. Kubernetes and your cluster
- 📖 From containers to orchestration: what a container runtime does, what it leaves to you, what an orchestrator adds.
- 📖 The control plane and the nodes: API server, scheduler, controllers, datastore (SQLite/etcd in K3s), kubelet,
  kube-proxy, the container runtime; how this lab maps onto them (a-cp1, b-w1, c-w1).
- 📖 The desired-state loop: you declare, controllers reconcile.
- ❓ Architecture check (8)
- ⌨ **Meet your cluster** (new, 15 min): `get nodes -o wide`, `cluster-info`, `api-resources`, find which node runs
  CoreDNS, count the Traefik pods. Checks: quizzes on what was found (the node of CoreDNS, the number of nodes) plus a
  probe that the learner's kubeconfig context works. Needs: k3s.

### 2. Talking to the API with kubectl
- 📖 Objects, `spec` and `status`; API groups and versions; `kubectl explain`.
- 📖 Imperative and declarative: `run`/`create` versus `apply -f`; why manifests in git win.
- 📖 Labels, selectors and annotations.
- 📖 Output formats: `-o wide|yaml|json|jsonpath|custom-columns`.
- ❓ kubectl check (10)
- ⌨ **First objects** (new, 20 min): create a pod imperatively, dump it to YAML, write a manifest with two labels,
  `apply` it, select pods by label, delete by label. Checks: pod `hello` Running with labels `app=hello,tier=web` in
  `${ns}`; no pod left with `tier=old`.
- ⌨ **Nodes and regions** (existing): topology labels, labelling a node, why the demo is one pod per region.
- 💻 **First pod** (easy): write a Pod manifest `hello` with two labels, a port and resource requests. Tests: Running, labels, port, requests set.

### 3. Pods
- 📖 What a pod is: shared network and volumes, why one process per container.
- 📖 Pod lifecycle: phases, container states, restart policies, `CrashLoopBackOff` and its back-off.
- 📖 Multi-container pods: sidecars, init containers.
- 📖 Reading `describe` and events.
- ❓ Pods check (10, includes "what is wrong in this `describe` output")
- ⌨ **Debug a crashing pod** (new, 20 min): setup applies a pod whose command exits 1 and one whose image tag does
  not exist. The learner finds both with `get`/`describe`/`logs`, fixes the command, fixes the tag. Checks: both
  pods Running, restart count stops growing.
- ⌨ **Init containers** (new, 15 min): a pod whose init container waits for a Service that does not exist yet; the
  learner creates the Service. Checks: pod Initialized and Running.
- 💻 **Sidecar logger** (easy): a pod with an app container writing a log file and a sidecar tailing it from a shared `emptyDir`. Tests: both containers Running, `kubectl logs -c sidecar` shows the app's lines.

### 4. Deployments and ReplicaSets
- 📖 Why not bare pods: ReplicaSets, Deployments, the pod template hash.
- 📖 Rolling updates: `maxSurge`, `maxUnavailable`, `rollout status/history/undo`.
- 📖 Recreate versus RollingUpdate; what readiness has to do with it (preview of course 2).
- ❓ Deployments check (10)
- ⌨ **Roll out and roll back** (new, 20 min): deploy `web` v1, update to v2, update to a broken v3 (image does not
  exist), observe the stalled rollout, `undo`. Checks: Deployment at revision with image v2, all replicas ready, no
  pods from v3.
- ⌨ **Scale across regions** (existing).
- 💻 **Zero-downtime rollout** (medium): a Deployment that never drops below 3 ready pods during a rollout and only serves ready pods (`maxUnavailable: 0`, readiness probe). Tests: 3 ready; a grader-triggered rollout keeps ≥ 3 ready throughout.

### 5. Services and DNS
- 📖 Services: ClusterIP, the endpoints behind it, kube-proxy's rules.
- 📖 NodePort and how traffic reaches a node; why LoadBalancer is off here (servicelb disabled) and what Traefik
  does instead.
- 📖 Service discovery: CoreDNS, `<svc>.<ns>.svc.cluster.local`, search domains.
- ❓ Services check (10)
- ⌨ **Expose and discover** (new, 20 min): expose `web` with a ClusterIP Service, call it by name from a client
  pod, break the selector and watch the endpoints empty, fix it. Checks: `http` from a pod to `web.${ns}` succeeds;
  EndpointSlice has 3 addresses.
- 💻 **Find me by name** (easy): a Service selecting a provided Deployment on a named port, reachable as `api.${ns}:8080`. Tests: http from a client pod by DNS name.

### 6. Namespaces and the shape of a cluster
- 📖 Namespaces, what is and is not namespaced, contexts.
- 📖 Where the system lives: `kube-system`, Traefik, CoreDNS, local-path.
- 💻 **Wrong selector** (easy): fix three provided manifests whose labels and selectors do not match. Tests: Deployment ready, Service has endpoints.
- ❓ Final quiz: Kubernetes Fundamentals (25 drawn from 40, pass 70 %)

**Course 1 total:** 19 lectures, 6 quizzes, 8 labs (2 existing), 5 challenges.

---

## Course 2: Configuration and Workloads (intermediate)

*Configure applications, size them, keep them healthy and give them storage.* Requires course 1. Needs: `k3s`.

### 1. ConfigMaps and Secrets
- 📖 Configuration outside the image; env versus mounted files; updates (files refresh, env does not).
- 📖 Secrets: base64 is not encryption; what K3s stores; when to use an external store.
- ❓ Configuration check (8)
- ⌨ **Configure an app** (new, 20 min): mount a ConfigMap as a file and a Secret as env, change the ConfigMap and
  see the file change without a restart, see the env not change. Checks: `run` in the pod reads the new file value;
  env still the old value; the Secret is not in the pod spec in clear.
- 💻 **Config from files** (easy): a ConfigMap built from two files mounted at `/etc/app`, and one key exposed as env. Tests: files present with the right content; env set.

### 2. Resources
- 📖 Requests and limits; how the scheduler uses requests; CPU throttling versus memory OOM kill.
- 📖 QoS classes; eviction under node pressure (this lab: `eviction-hard memory.available<64Mi`).
- 📖 LimitRange and ResourceQuota.
- ❓ Resources check (10)
- ⌨ **Out of memory** (new, 20 min): a pod with a 32 Mi limit allocates 64 Mi and is OOMKilled; raise the limit;
  then a namespace quota rejects a fourth replica. Checks: pod Running with limit ≥ 96 Mi, last termination reason
  seen = OOMKilled (event), quota exists and `used.pods` ≤ `hard.pods`.
- 💻 **Fit the quota** (medium): size requests/limits of a 3-replica Deployment so it fits a provided ResourceQuota and LimitRange and stays Guaranteed QoS. Tests: admitted, 3 ready, QoS `Guaranteed`.

### 3. Health probes
- 📖 Liveness, readiness, startup probes; what each one triggers.
- 📖 Readiness and Services: a not-ready pod leaves the endpoints.
- ❓ Probes check (8)
- ⌨ **Probes that lie** (new, 20 min): a Deployment whose liveness probe hits the wrong path restarts forever; fix
  it; then make one pod unready (touch a file the readiness probe checks) and watch it leave the endpoints. Checks:
  restarts stable, EndpointSlice has 2 ready of 3 while the file is there.
- 💻 **Honest probes** (medium): startup, liveness and readiness probes for a slow-starting app (provided image starts in 20 s). Tests: no restarts during start; not ready before 20 s; ready after.

### 4. Jobs and CronJobs
- 📖 Jobs: completions, parallelism, backoffLimit; CronJobs: schedule, concurrency, history.
- ❓ Batch check (6)
- ⌨ **Batch work** (new, 15 min): a Job computing 5 completions with parallelism 2, a CronJob every minute with
  `concurrencyPolicy: Forbid`. Checks: Job Complete with 5 succeeded; ≥ 2 CronJob runs recorded.
- 💻 **Nightly report** (easy): a CronJob with a schedule, history limits and `Forbid`, triggered once by the grader. Tests: spec fields; the triggered Job completes.

### 5. Storage and StatefulSets
- 📖 Volumes, PersistentVolumes, PersistentVolumeClaims, StorageClasses, `WaitForFirstConsumer`.
- 📖 local-path: a volume lives on one node, so in this lab **in one region**; what that means when the region fails.
- 📖 StatefulSets: stable names, ordered rollout, per-pod claims.
- ❓ Storage check (10)
- ⌨ **Data that survives** (new, 25 min): a StatefulSet of 2 with a claim each writes its hostname to its volume;
  delete a pod, see it come back with the same name and data; find which region each volume is in. Checks: pod
  `db-0` data file intact after deletion; quiz on the region of `db-1`'s volume (read from the PV's node affinity).
- ⌨ **Database across the WAN** (new, 20 min, needs `ci`): the reference webapp's PostgreSQL lives in region a;
  measure query time from each region (`/app` reports it), reason about read replicas. Checks: http from each site's
  ingress; quiz on why c is slowest.
- 💻 **Counter that remembers** (hard): a StatefulSet of 2 with per-pod claims and a headless Service; each pod's counter survives deletion. Tests: stable DNS names `counter-0.counter`; value kept after the grader deletes `counter-1`.

### 6. DaemonSets and the node
- 📖 DaemonSets (Traefik here is one per site); node-level agents; tolerating control-plane taints.
- 📖 Static pods and the kubelet (where K3s puts its manifests).
- ❓ Final quiz: Configuration and Workloads (25 drawn from 40)

**Course 2 total:** 13 lectures, 6 quizzes, 6 labs, 5 challenges.

---

## Course 3: Networking and Scheduling across Regions (intermediate → advanced)

*Route traffic in, control it inside, place pods deliberately and survive a region going dark.* Requires course 2.
Needs: `k3s`, `vpn`; stretched layout (multi-cluster sections say so).

### 1. Ingress
- 📖 Ingress resources and controllers; Traefik in this lab (hostPort per site, one per region).
- 📖 Host and path routing, TLS termination; Gateway API as the successor.
- ❓ Ingress check (8)
- ⌨ **Route traffic in** (new, 20 min): expose two Deployments under `/a` and `/b` with one Ingress, reach them via
  every site's public IP. Checks: `http` from a node in each site to `${public:<site>}/a` returns the right app.
- 💻 **Two paths, one Ingress** (easy): route `/shop` and `/api` to two provided Services with path rewriting. Tests: both paths answer through every site's ingress.

### 2. Network policies
- 📖 Default allow; NetworkPolicy ingress/egress; selectors; why DNS needs an egress rule.
- 📖 How K3s enforces them (embedded kube-router netpol controller over flannel).
- ❓ NetworkPolicy check (8)
- ⌨ **Default deny** (new, 25 min; **spike S1** first): deny all ingress in the namespace, allow only
  `role=frontend` to reach `api` on 8080. Checks: http from a `frontend` pod succeeds; from another pod it times out.
- 💻 **Lock it down** (medium; after S1): NetworkPolicies so that only `web` reaches `api`, `api` reaches only `db` and DNS. Tests: the allowed paths answer, five denied paths time out.

### 3. Scheduling
- 📖 nodeSelector, node affinity (required and preferred).
- 📖 Pod affinity and anti-affinity; topology keys (hostname, zone, region).
- 📖 Taints and tolerations; `NoSchedule` versus `NoExecute`.
- ❓ Scheduling check (10)
- ⌨ **Put it where it belongs** (new, 25 min): pin a cache to region c with node affinity, keep two replicas of an
  API off the same region with anti-affinity, taint b-w1 `dedicated=batch:NoSchedule` and run a batch Job there
  with a toleration. Cluster change: a taint on b-w1 (removed by teardown). Checks: pods' nodes match each rule.
- 💻 **One per region** (medium): a 3-replica Deployment with required anti-affinity on the region key, staying on workers only. Tests: three regions, none on the control plane; a fourth replica stays Pending.

### 4. Spreading over failure domains
- 📖 Topology spread constraints: `maxSkew`, `whenUnsatisfiable`, `minDomains`, `nodeTaintsPolicy`,
  `matchLabelKeys` (the rollout trap measured in Phase 10).
- ⌨ **Scale across regions** (existing, reused).
- ❓ Spreading check (6)

### 5. When a region fails
- 📖 Node heartbeats, NotReady, taint-based eviction and tolerationSeconds.
- 📖 PodDisruptionBudgets, `drain` and `cordon`: planned versus unplanned disruption.
- ❓ Failure check (8)
- ⌨ **Losing a site** (existing).
- ⌨ **Drain with a budget** (new, 20 min): a Deployment of 3 with a PDB `minAvailable: 2`; drain c-w1 and watch the
  drain wait; uncordon. Cluster change: cordon/drain c-w1 (uncordoned by teardown). Checks: never fewer than 2 ready
  during the drain (probe samples), node schedulable again at the end.
- 💻 **Survive a drain** (medium): a Deployment and PDB that keep 2 of 3 available through a grader-run drain of one node. Tests: never fewer than 2 ready; drain completes.

### 6. Traffic that stays local, and global names
- 📖 `trafficDistribution: PreferSameZone`; latency between regions (course "Cloud networking basics").
- 📖 Global load balancing: geo-aware DNS in this lab (`www.global.lab`).
- ⌨ **Stay in your region** (new, 20 min): call the demo from each site, read which region served it, scale the
  demo to 0 in one region and see traffic cross the WAN (latency jumps). Checks: `http` from c's node served in c,
  then (after the change) served elsewhere with p50 above the b-c RTT.
- 💻 **Local first** (hard): spread a Deployment one per region, keep traffic in the caller's zone, and survive one region's pods being deleted. Tests: each site served locally; after the grader deletes c's pod, c still gets answers within 2× the b-c RTT.
- ❓ Final quiz: Networking and Scheduling across Regions (25 drawn from 40)

**Course 3 total:** 12 lectures, 6 quizzes, 7 labs (2 existing), 5 challenges.

---

## Course 4: Production Kubernetes (advanced)

*Secure, scale, package, deliver, observe and troubleshoot.* Requires course 3. Needs: `k3s`, `vpn`; sections need
`monitoring`, `ci` or the multi-cluster layout as marked.

### 1. Access control
- 📖 Authentication versus authorization; users, ServiceAccounts, tokens.
- 📖 RBAC: Role, ClusterRole, bindings; `kubectl auth can-i`.
- ❓ RBAC check (10)
- ⌨ **Least privilege** (new, 25 min): create a ServiceAccount that can only read pods in `${ns}`, mint a token,
  use it with kubectl, prove it cannot delete. Checks: `auth can-i --as=system:serviceaccount:...` list=yes,
  delete=no, secrets=no.
- 💻 **Read-only robot** (medium): a ServiceAccount, Role and RoleBinding for a CI bot that may list and watch Deployments and pods and read logs, nothing else. Tests: `auth can-i` matrix of 8 verbs/resources.

### 2. Pod security
- 📖 securityContext: non-root, read-only root filesystem, capabilities, seccomp.
- 📖 Pod Security Admission levels (privileged, baseline, restricted) and namespace labels.
- ❓ Pod security check (8)
- ⌨ **Restricted namespace** (new, 20 min; spike S3): label the namespace `restricted`, see a root pod rejected, fix
  its securityContext until it is admitted. Checks: namespace label set; pod Running with `runAsNonRoot`,
  `allowPrivilegeEscalation: false`, dropped capabilities.
- 💻 **Restricted-ready** (medium): make a provided Deployment admissible under `restricted` without changing its image. Tests: admitted in a restricted namespace, 3 ready.

### 3. Autoscaling
- 📖 metrics-server and the Metrics API; HorizontalPodAutoscaler; scaling behaviour and stabilisation.
- 📖 Cluster autoscaling and why it does not apply to a fixed lab.
- ❓ Autoscaling check (8)
- ⌨ **Scale on load** (new, 25 min; needs metrics-server, spike S2): an HPA on CPU 50 %, a load generator pod,
  watch replicas grow and shrink back. Checks: replicas ≥ 3 under load; `currentMetrics` present.
- 💻 **Autoscale me** (hard; needs metrics-server): an HPA with a scale-down stabilisation window and a max, plus the resource requests it needs. Tests: under grader load reaches ≥ 3; never above the max; scales down only after the window.

### 4. Packaging: Helm and Kustomize
- 📖 Kustomize: bases, overlays, patches (built into kubectl).
- 📖 Helm: charts, values, releases, upgrade and rollback.
- ❓ Packaging check (8)
- ⌨ **One app, three regions** (new, 25 min): a Kustomize base with an overlay per region (replicas, a region env
  var), then install a small chart with Helm from the host and upgrade a value. Checks: three Deployments with the
  right env; Helm release at revision 2.
- 💻 **Overlay per region** (medium): a Kustomize base and three overlays (`a`, `b`, `c`) setting replicas and a `REGION` env; the grader builds and applies them. Tests: `kustomize build` succeeds for each; applied Deployments carry the right values.

### 5. GitOps
- 📖 Pull-based delivery; Flux sources and kustomizations; drift correction.
- ❓ GitOps check (6)
- ⌨ **Ship by commit** (new, 25 min, needs `ci`): change the webapp in Gitea, watch the pipeline build and Flux roll
  it out to every region (Phase 7's `ci-rollout`, as a guided lab); then `kubectl edit` the Deployment and watch Flux
  revert it. Checks: new version served in every region; replicas back to the Git value.

### 6. Observability
- 📖 Metrics, logs, events; kube-state-metrics; reading the lab's WAN and cluster dashboards.
- ❓ Observability check (6)
- ⌨ **Find it in the graphs** (new, 20 min, needs `monitoring`): setup degrades a link silently; the learner finds
  which pair is slow in Grafana and names it. Checks: quiz answer matches the degraded link (setup picks it).

### 7. Troubleshooting (capstone)
- 📖 A method: from symptom to layer (scheduling, image, config, network, DNS, node).
- ⌨ **Everything is broken** (new, 40 min, `reset: snapshot`): setup breaks five things in an app's namespace and
  one node (a wrong selector, a missing ConfigMap key, a NetworkPolicy blocking DNS, a bad image, a cordoned node);
  each step names a symptom, not the cause. Checks: the app answers from every region.
- 💻 **Broken manifests** (hard): five provided manifests with one bug each (API version, selector, probe port, volume name, policy); make the app work. Tests: dry-run clean; app answers through every ingress.
- ❓ Final exam: Production Kubernetes (30 drawn from 60, 45 min, pass 75 %)

### 8. Multi-cluster (optional, multi-cluster layout)
- 📖 One cluster or many: blast radius, latency, upgrades; Skupper's application network.
- ⌨ **Lose a cluster** (new, 25 min, layout multi-cluster): serve from three clusters, take one down, measure failed
  requests (Phase 8's `cluster-failover`, guided). Checks: ≤ 2 failed requests; service answers from the others.

**Course 4 total:** 12 lectures, 7 quizzes, 8 labs, 5 challenges.

---

## The existing network labs

*Cloud networking basics* (beginner, no prerequisites) keeps Regions and latency, Private networks and Routing
around failure as one course of three sections with lectures and quizzes added around them. Course 3 lists it as
recommended.

## Lab RAM budget

Lab workloads are small (whoami-style images, 16-64 Mi each). Rule for authors: a lab adds at most **300 MiB**
requests across the cluster and runs on the stretched layout's default sizes (server 1536 MiB, agents 768 MiB).
The HPA and capstone labs are measured in their milestone; if one does not fit, it shrinks before the nodes grow.
