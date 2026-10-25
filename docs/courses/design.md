# Courses: screens and interactions

Layouts are borrowed from products learners already know, one per job:

| Screen | Borrowed from | Why that one |
|---|---|---|
| Catalogue, course page, course player (lectures) | **Udemy** | The reference for "a course is sections of lectures, with a curriculum sidebar and progress". |
| Quiz, final exam | **LeetCode** (problem screen: statement left, answer area right, submit → result panel) and Udemy's quiz flow | Clear split between what is asked and how you answer; immediate, itemised results. |
| Coding challenge | **LeetCode** problem screen: description / solution / submissions tabs left, code editor right, test cases and result console below the editor, Run and Submit | The pattern for "write code, run it against tests, submit". Here the code is a Kubernetes manifest. |
| Lab | **Killercoda** (steps left with click-to-run commands, terminal right with an editor tab, a Check button per step) | The established pattern for "do it in a real terminal while reading instructions". |
| Editor | **VS Code** (explorer, tabs, breadcrumbs, command palette, diff), through Monaco | The editor learners already know; frontend.md "Editor". |

What is **not** borrowed: logos, names, brand colours (Udemy purple, LeetCode orange) or illustrations. Visual identity
stays Phase 10's (`web/app.css` tokens): enamel/paper/ink palette, lamps as the only colour, Overpass for UI, Source
Serif 4 for reading, JetBrains Mono in terminals and code. The mimic panel (live map of the lab) stays the one bold
element and appears where the lab matters: the course page header and the lab screen.

Implementation stack (frontend.md): React + Vite, shadcn/ui primitives, borrowed registry components (Aceternity,
Magic UI, Origin UI, Kibo UI), Monaco, xterm.js. Each screen below names the components that build it.

## Routes

| Route | Screen |
|---|---|
| `/` | Home: resume card with the course's line, streak (five weeks of lamps) and level, next on your line, your courses, recent activity |
| `/roadmap` | Roadmap (AlgoMonster's learning path as the mimic panel): every course a line off one trunk, every section a station; `?course=` opens a course's first station |
| `/courses` | Catalogue: all courses, filtered by level |
| `/courses/$course` | Course page (landing): overview, what you'll learn, requirements, curriculum accordion |
| `/practice` | Practice (LeetCode's problem list): every lab, challenge and quiz, filters, "Pick one for me", solved ring, progress by topic |
| `/learn/$course/$item` | Course player: the item (lecture, quiz, lab or challenge) plus the curriculum sidebar |
| `/labs/$lab` | A lab outside any course (labs not yet placed in a course) |
| `/profile`, `/settings` | Profile (LeetCode's: level, a year of practice, solved, badges, courses); theme, terminal, editor and lab preferences |

The header: Home, Roadmap, Courses, Practice; search (Ctrl+K, the command palette); streak and level (to the
profile); theme. Screens whose backends do not exist yet (rewards, labstate, practice, streaks, quiz attempts, lab
sessions, challenge runs) read `web/src/mock/`, shaped as the API will serve them: hooking a screen up replaces that
import with a query. The roadmap is the page's one bold element; the station you are at is the only thing that
moves (`station-current`, off with reduced motion).

History-mode routes (TanStack Router); the Go server answers unknown non-API paths with `index.html`.

## 1. Catalogue (Udemy "My learning" + course cards)

```
┌ OpsForge Labs ─ Courses  Badges ───────────────── ○ Lab running, stretched   800 XP ┐
│                                                                                     │
│  Continue learning                                                                  │
│  ┌──────────────────────────┐ ┌──────────────────────────┐                          │
│  │ Kubernetes Fundamentals  │ │ Cloud networking basics  │                          │
│  │ 3.2 Pod lifecycle        │ │ Completed                │                          │
│  │ ███████░░░░░░  48 %      │ │ █████████████ 100 %      │                          │
│  │ [Resume]                 │ │ [Review]                 │                          │
│  └──────────────────────────┘ └──────────────────────────┘                          │
│                                                                                     │
│  All courses                         Level: [All ▾]  Needs: [Runs on this lab ▾]    │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐               │
│  │ ▓ track art ▓│ │ ▓ track art ▓│ │ ▓ track art ▓│ │ ▓ track art ▓│               │
│  │ Kubernetes   │ │ Configuration│ │ Networking & │ │ Production   │               │
│  │ Fundamentals │ │ and Workloads│ │ Scheduling   │ │ Kubernetes   │               │
│  │ Beginner     │ │ Intermediate │ │ Intermediate │ │ Advanced     │               │
│  │ 6 sections   │ │ 6 sections   │ │ 6 sections   │ │ 8 sections   │               │
│  │ 8 labs, 6 h  │ │ 6 labs, 6 h  │ │ 7 labs, 7 h  │ │ 8 labs, 9 h  │               │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘               │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

- Card = Udemy's: image area, title, one-line subtitle, level, counts, total time; progress bar if started.
- The "image" is generated, not stock art: a small schematic in the panel's style (a course's sections drawn as
  stations on a line, passed ones lit). Same component everywhere a course needs a picture.
- Filters: level, and "Runs on this lab" (hides courses whose `layout`/`needs` the running topology can't meet,
  e.g. multi-cluster sections).
- A course that needs another first shows "After Kubernetes Fundamentals" instead of a lock icon.
- Built from: shadcn Card, Progress, Select (filters), Badge; Aceternity Card Hover Effect (subtle); the course art is
  our own SVG component (shared with the course page roadmap).

## 2. Course page (Udemy course landing)

```
┌───────────────────────────────────────────────────────────────────────────────────┐
│ Courses › Kubernetes                                                              │
│ Kubernetes Fundamentals                                     ┌───────────────────┐ │
│ Run, expose and debug workloads on a cluster spread over    │  mimic panel      │ │
│ three regions.                                              │  (live lab map)   │ │
│ Beginner, 6 sections, 19 lectures, 6 quizzes, 8 labs       │                   │ │
│                                                             │ [Start course]    │ │
│ ┌ What you'll learn ──────────────────────────────────────┐ │ Needs: K3s,       │ │
│ │ ✓ Read the cluster with kubectl   ✓ Debug a failing pod │ │ stretched layout  │ │
│ │ ✓ Roll out and roll back          ✓ Expose with Services│ │ ✓ this lab can    │ │
│ └─────────────────────────────────────────────────────────┘ │   run it          │ │
│                                                             └───────────────────┘ │
│ Course content           6 sections · 36 items · 6 h 10 min   [Expand all]        │
│ ▾ 1  Kubernetes and your cluster                       4 items · 40 min   ✓ 4/4   │
│     ▤ Lecture  From containers to orchestration                 8 min   ✓         │
│     ▤ Lecture  The control plane and the nodes                 10 min   ✓         │
│     ☐ Quiz     Architecture check (8 questions)                 5 min   ✓ 7/8     │
│     ⌨ Lab      Meet your cluster                               15 min   ✓         │
│ ▸ 2  Talking to the API with kubectl                   6 items · 70 min   2/6     │
│ ▸ 3  Pods                                              7 items · 80 min           │
│ ...                                                                               │
│ Requirements                                                                      │
│ - Basic shell (cd, ls, a text editor). No prior Kubernetes.                       │
│ Description                                                                       │
│ (Markdown)                                                                        │
└───────────────────────────────────────────────────────────────────────────────────┘
```

- Header and sticky right card as on Udemy; the card's media slot holds the live mimic panel instead of a video
  preview, plus the course's requirements checked against the running topology (green lamp or an amber line saying
  which layout to switch to).
- Curriculum accordion exactly as Udemy: section rows with item count, duration and progress; item rows with a type
  icon, title, duration and a tick. Items can be opened in any order (Udemy does not lock items); labs keep their own
  prerequisites (`requires`) because some build on a previous lab's state.
- The CTA reads "Start course", then "Continue: 3.2 Pod lifecycle".
- Built from: shadcn Breadcrumb, Accordion, Card (sticky), Button; Aceternity Timeline restyled as the course
  roadmap below "What you'll learn"; the mimic panel (labmap feature).

## 3. Course player: lecture (Udemy player)

```
┌ ‹ Kubernetes Fundamentals ─────────────────── 48 % ████░░░ ─ [Prev] [Next] ┐
│ ┌───────────────────────────────────────────────┐ ┌ Course content  ✕ ──┐ │
│ │ 3.2  Pod lifecycle                            │ │ ▾ 3 Pods        3/7  │ │
│ │                                               │ │  ✓ 3.1 What a pod is │ │
│ │  (article, serif, ≤ 70 ch, diagrams)          │ │  ● 3.2 Pod lifecycle │ │
│ │                                               │ │  ○ 3.3 Quiz          │ │
│ │  ┌────────────────────────────────┐           │ │  ○ 3.4 Lab: crash... │ │
│ │  │ kubectl get pod web -w   [Copy]│           │ │ ▸ 4 Deployments      │ │
│ │  └────────────────────────────────┘           │ │ ▸ 5 Services         │ │
│ │                                               │ │                      │ │
│ │  [Mark as complete]          [Next: Quiz ›]   │ │                      │ │
│ └───────────────────────────────────────────────┘ └──────────────────────┘ │
│  Overview | Notes | Resources                                              │
└────────────────────────────────────────────────────────────────────────────┘
```

- Udemy's player chrome: top bar with the course title (back to the course page), progress, Prev/Next; the content
  area; the collapsible "Course content" sidebar on the right with sections, ticks and the current item.
- Lecture content is Markdown: headings, lists, tables, admonitions (`> **Note**`), images and inline SVG diagrams
  from the pack's `assets/`, and command blocks. Command blocks offer **Copy** (lectures have no terminal) and, when
  a terminal is open in another tab, nothing more: lectures stay reading.
- Tabs under the content as on Udemy, reduced to what works locally: **Overview** (course description),
  **Notes** (per-item notes saved in the DB, Udemy's timestamped notes without the video), **Resources** (links and
  files the lecture lists).
- "Mark as complete" (Udemy marks lectures complete when you reach the end); reaching the bottom of a lecture also
  ticks it.
- Keyboard: `←`/`→` for Prev/Next as on Udemy.
- Built from: shadcn Sidebar (right, collapsible), Tabs, Progress, Button; react-markdown + Shiki + Mermaid; notes
  with react-hook-form + shadcn Textarea (autosave).

## 4. Course player: quiz (LeetCode problem layout, Udemy quiz flow)

```
┌ ‹ Kubernetes Fundamentals ─────────────────── 48 % ████░░░ ─ [Prev] [Next] ┐
│ ┌ Quiz 3.3 · Pods · 8 questions · pass 70 % ──────────────────────────────┐│
│ │ ┌ Question 3 of 8 ───────────────────┐ ┌ Your answer ──────────────────┐ ││
│ │ │ A pod's container exits with code  │ │ ( ) Running                   │ ││
│ │ │ 1 every few seconds. Which status  │ │ ( ) Pending                   │ ││
│ │ │ will `kubectl get pods` show after │ │ (•) CrashLoopBackOff          │ ││
│ │ │ a few restarts?                    │ │ ( ) ImagePullBackOff          │ ││
│ │ │                                    │ │                               │ ││
│ │ │ ```                                │ │ [Skip]            [Next ›]    │ ││
│ │ │ kubectl get pods                   │ └───────────────────────────────┘ ││
│ │ │ ```                                │  ① ② ●③ ④ ⑤ ⑥ ⑦ ⑧  [Submit quiz] ││
│ │ └────────────────────────────────────┘                                   ││
│ └──────────────────────────────────────────────────────────────────────────┘│
└────────────────────────────────────────────────────────────────────────────┘
After submit:
│ ┌ Result: 7 of 8 (88 %) · passed ─────────────────────────────────────────┐ │
│ │ ✓ 1  What runs the containers on a node?                                 │ │
│ │ ✗ 4  Which restartPolicy ... You chose Never. Answer: OnFailure.         │ │
│ │       Why: Jobs need ... (explanation, links back to lecture 3.2)        │ │
│ │ [Retake quiz]  [Review lecture 3.2]  [Next: Lab ›]                       │ │
│ └──────────────────────────────────────────────────────────────────────────┘ │
```

- LeetCode's split: the question (statement, code snippets, YAML) on the left, the answer area on the right, a
  question navigator (numbered dots, answered ones filled) and **Submit** at the bottom; LeetCode's result panel
  after submitting: score, then every question with right/wrong and the explanation.
- Udemy's quiz behaviour: one question at a time, skip and come back, results at the end, retake any time, wrong
  answers point to the lecture that covers them (`see:` per question).
- Question types: single choice (radio), multiple choice ("select all that apply", checkboxes; all-or-nothing
  scoring), and **command output**: "here is `kubectl describe` output, what is wrong?" (single choice over a code
  block; the most useful kind for Kubernetes).
- Final exams (end of each course): 20-30 questions drawn from the course's sections, optionally timed (a countdown
  in the top bar, like LeetCode contests), shuffled order and choices.
- Answers are scored on the server; the page never receives the key (Phase 10 rule).
- Built from: shadcn Resizable (split), RadioGroup, Checkbox, Toggle Group (navigator), Alert (results), Progress;
  a countdown in the top bar for timed exams.

## 5. Course player: lab (Killercoda)

```
┌ ‹ Kubernetes Fundamentals ── Lab 3.4 Debug a crashing pod ── ● Step 2/4 ── [Course content ☰] ┐
│ ┌ Steps (40 %) ─────────────────────────┐ ┌ [Terminal] [Lab map] ───────────── ● Connected ┐ │
│ │ Debug a crashing pod     15 min 150 XP │ │ lab OpsForge $ opsforge kubectl -n lab-crash   │ │
│ │ ✓ 1 Find the failing pod               │ │   get pods                                     │ │
│ │ ● 2 Read its logs                      │ │ NAME   READY  STATUS            RESTARTS       │ │
│ │   The container exits right after...   │ │ web    0/1    CrashLoopBackOff  4              │ │
│ │   ┌───────────────────────────────┐    │ │ lab OpsForge $ ▌                               │ │
│ │   │ opsforge kubectl logs web  ▶  │    │ │                                                │ │
│ │   └───────────────────────────────┘    │ │                                                │ │
│ │   Hint 1 of 2 (costs 15 XP)            │ │                                                │ │
│ │   [Check]                              │ │                                                │ │
│ │ ○ 3 Fix the command                    │ │                                                │ │
│ │ ○ 4 Explain the restart delay          │ │                                                │ │
│ │                                        │ │                                                │ │
│ │ Reset lab · Abandon                    │ │                                                │ │
│ └────────────────────────────────────────┘ └────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────────────────────────┘
```

- Killercoda's split: instructions left with a draggable divider, terminal right. Its "click a command to run it"
  becomes a ▶ on each command block that **types and runs** it in the terminal (Killercoda's behaviour); Phase 10's
  "Paste in terminal" (types without Enter) stays for multi-line blocks.
- Killercoda's one-step-at-a-time flow with a **Check** button that must pass before the next step opens; failed
  checks show the Phase 10 itemised result ("wanted / found").
- Right pane tabs: **Terminal**, **Editor** and **Lab map** (the mimic panel, full size); a lamp in the tab lights
  when a link changes, so you notice a fault without switching. The Editor tab is Killercoda's editor tab done with
  Monaco: the attempt's workspace files (manifests the lab provides or the learner creates), shared with the
  terminal's working directory. A lab step can open a file in the editor (`open: web.yaml` in the step).
- The course sidebar collapses to a ☰ button here: labs need the width.
- **Reset lab** (new): restores the lab's starting point (setup again, or a snapshot for labs that change the
  cluster widely; content-model.md "Reset").
- On pass: Phase 10's debrief, XP and badges, then "Next: 4.1 ..." as in Udemy's player.
- Built from: shadcn Resizable, Tabs, Alert, Collapsible (hints), a vertical stepper (Origin UI/Kibo UI); xterm.js;
  Monaco (editor feature); Magic UI Animated Beam on the lab map while a check runs.

## 5b. Course player: coding challenge (LeetCode)

```
┌ ‹ Kubernetes Fundamentals ── Challenge 4.5 Zero-downtime rollout ── Medium ── [☰] ───────────────────┐
│ ┌ [Description] [Solution 🔒] [Submissions] ┐ ┌ web.yaml  service.yaml ─────────────── YAML  ⚙ ┐ │
│ │ Zero-downtime rollout          Medium     │ │  1 apiVersion: apps/v1                          │ │
│ │                                           │ │  2 kind: Deployment                             │ │
│ │ Write a Deployment `web` (3 replicas,     │ │  3 metadata:                                    │ │
│ │ image traefik/whoami:v1.11.0) that never  │ │  4   name: web                                  │ │
│ │ has fewer than 3 ready pods during a      │ │  5 spec:                                        │ │
│ │ rollout, and only sends traffic to pods   │ │  6   replicas: 3                                │ │
│ │ that answer on /health.                   │ │  7   strategy:▌      ← completion: rollingUpdate │ │
│ │                                           │ │       ~~~~~~~~ maxSurge is required ...         │ │
│ │ Constraints                               │ ├─────────────────────────────────────────────────┤ │
│ │ - namespace is provided (${ns})           │ │ [Test cases] [Result]                           │ │
│ │ - requests ≤ 50m CPU, 32Mi memory         │ │  Case 1 ✓ 3 replicas ready                      │ │
│ │                                           │ │  Case 2 ✓ readiness probe on /health            │ │
│ │ Example                                   │ │  Case 3 ✗ ready pods during rollout: min 2,     │ │
│ │ (expected kubectl output)                 │ │           wanted ≥ 3 (set maxUnavailable: 0)    │ │
│ │                                           │ │                     [Run]  [Submit]             │ │
│ └───────────────────────────────────────────┘ └─────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────────────────────────────┘
```

- LeetCode's layout one-to-one: left tabs **Description** (statement, constraints, examples), **Solution** (locked
  until accepted; then the reference manifest and an explanation, with a Monaco diff against yours), **Submissions**
  (history with verdict and time); right: editor with file tabs and language label, below it a console with **Test
  cases** and **Result**.
- **Run** = schema validation (monaco-yaml markers) + `kubectl apply --dry-run=server` (admission, defaults, quota)
  → diagnostics in the console and as editor markers. Nothing touches the cluster.
- **Submit** = apply into a fresh scratch namespace, run the challenge's tests (probe steps, each test case one
  expectation group), then delete the namespace. Verdicts as on LeetCode: **Accepted**, **Wrong answer** (which case
  failed, wanted/found), **Invalid manifest** (dry-run errors), **Time limit exceeded** (pods never Ready within
  the challenge's limit). "Runtime" shows time to Ready.
- Difficulty labels Easy / Medium / Hard (LeetCode), shown with lamp colours (green, amber, red).
- Starter files come from the challenge; the workspace persists per learner and challenge (come back later, your
  draft is there); **Reset to starter** in the editor menu.
- Built from: shadcn Resizable (three panes), Tabs, Badge, Table (submissions), Alert; Monaco (editor feature,
  including DiffEditor); Sonner for verdict toasts.

## 6. Behaviour shared by all screens

- **Progress**: a lecture is complete when marked or read to the end; a quiz when passed (best score kept); a lab
  when passed; a challenge when accepted. Course progress = completed items / items. Udemy-style section counters "3/7".
- **Resume**: the catalogue and course page link to the last item opened.
- **Completion**: finishing every item of a course awards the course's badge and shows a completion page (a
  printable certificate of completion, clearly local: name from the users table, course, date, items).
- **Responsive**: below 900 px the player sidebar becomes a drawer, the quiz and lab splits stack (question above
  answers; steps above terminal at 70 vh), as in Phase 10.
- **Accessibility**: Phase 10's floor (visible focus, keyboard for every action, reduced motion, contrast); quiz
  choices are real radio/checkbox inputs; the navigator dots have labels ("Question 3, answered").
- **Copy**: sentence case, plain verbs. Buttons say what happens: "Submit quiz", "Retake quiz", "Mark as complete",
  "Reset lab", "Run", "Submit".
- **Command palette** (Ctrl+K, shadcn Command): jump to any course or item, run lab actions (Check, Reset, Heal the
  lab), editor actions when the editor has focus (F1 opens Monaco's own palette, as in VS Code).
