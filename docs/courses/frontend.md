# Frontend

## Stack

| Concern | Choice | Why |
|---|---|---|
| Framework | **React 19 + TypeScript (strict)** | The ecosystem every borrowed component targets. |
| Build | **Vite** (SPA) | The platform serves one origin from a Go binary; no SSR or SEO need (a local, signed-in app). **Next.js was considered**: its static export cannot serve runtime course/item ids without workarounds, and its server mode adds a Node process (~100 MB) and a hop in front of the Go API. Vite builds static assets Go embeds. If a public, multi-user site comes later, the same components move to Next. |
| Routing | **TanStack Router** (file-based, type-safe params and search) | Typed `/learn/$course/$item` routes, loaders that prefetch with TanStack Query. |
| Server state | **TanStack Query** + **openapi-fetch** (types from `openapi-typescript`) | Typed calls generated from the OpenAPI contract; caching, retries, optimistic updates (notes, mark complete). |
| Live updates | `EventSource` (SSE) → Query cache invalidation / direct cache writes | Lab state, progress, awards, check logs without polling. |
| Client state | **Zustand** (persisted slices: panel sizes, open editor tabs, sidebar collapsed) | Small, testable, no context soup. |
| Styling | **Tailwind CSS v4** + **shadcn/ui** (Radix primitives), CSS variables mapped to the Phase 10 tokens | Accessible primitives, components owned in the repo. |
| Forms | **react-hook-form + zod** | Notes, settings, quiz answers. |
| Icons | **lucide-react** | shadcn's default. |
| Motion | **motion** (Framer Motion), only through borrowed components and state changes | Restraint rule (design.md). |
| Markdown | **react-markdown + remark-gfm + rehype-sanitize**, code via **Shiki** (VS Code's TextMate grammars and themes), diagrams via lazily loaded **Mermaid** | Lectures come from packs at runtime (no MDX compile); Shiki matches the editor's highlighting. |
| Editor | **Monaco** (`monaco-editor` + `@monaco-editor/react`), **monaco-yaml** | Below. |
| Terminal | **@xterm/xterm** + addons `fit`, `web-links`, `search`, `unicode11`, `webgl` | VS Code's integrated terminal is xterm.js. |
| Fonts | Overpass, Source Serif 4, JetBrains Mono via **@fontsource** (self-hosted) | Works offline (Phase 10 depended on Google Fonts and a CDN). |
| Tests | Vitest, Testing Library, MSW (handlers generated from OpenAPI), Playwright | testing.md. |
| Quality | ESLint (typescript-eslint strict, react-hooks, jsx-a11y), Prettier, `tsc --noEmit`, knip (dead code) | CI gates. |
| Package manager | **pnpm** (installed: 10.6) | Fast, strict. |

## Components: borrow, don't build

Rule: if shadcn/ui, a shadcn block or a component registry has it, install it (`pnpm dlx shadcn add ...`, which
copies the source into `web/src/components/ui/` so we own and can adapt it) and restyle with our tokens. Hand-written
components are for what nobody ships: the mimic panel, the course map art, the check-result table.

| Need | Source |
|---|---|
| App shell, collapsible curriculum sidebar | shadcn **Sidebar** + shadcn blocks (`sidebar-07`-style) |
| Split panes (player, lab, challenge) | shadcn **Resizable** (`react-resizable-panels`) |
| Curriculum accordion | shadcn **Accordion** |
| Tabs (Overview/Notes/Resources, Terminal/Lab map, Description/Solution/Submissions) | shadcn **Tabs** |
| Course cards | shadcn **Card**; hover from **Aceternity** "Card Hover Effect" or "Focus Cards" (subtle variant) |
| Course roadmap on the course page | **Aceternity** "Timeline" (sections as stations), restyled as a track |
| Progress | shadcn **Progress**; XP counter **Magic UI** "Number Ticker" |
| Course completion moment | **Magic UI** "Confetti" (once, respects reduced motion) |
| Live traffic on the mimic panel links | **Magic UI** "Animated Beam" (beams along a link while a check or transfer runs) |
| Command palette (jump to course/item, run lab actions) | shadcn **Command** (cmdk) |
| Dialogs, drawers, popovers, tooltips, toasts | shadcn **Dialog**, **Sheet**, **Popover**, **Tooltip**, **Sonner** |
| Quiz choices | shadcn **RadioGroup**, **Checkbox**, **Toggle Group** (question navigator) |
| Steppers (lab steps) | **Origin UI** stepper (vertical) or shadcn-compatible stepper from **Kibo UI** |
| File tree in the editor | **react-arborist** (virtualised, keyboard, rename/drag) styled with shadcn tokens |
| Code blocks with copy/run | **Kibo UI** or **Magic UI** code block patterns + Shiki |
| Charts (quiz history, time per lab) | shadcn **Chart** (Recharts) |
| Empty/loading states | shadcn **Skeleton**, **Empty** patterns |

Aceternity and Magic UI components are copied through their shadcn registries (`pnpm dlx shadcn add
"https://ui.aceternity.com/registry/timeline.json"`, `@magicui/...`). Each borrowed component gets a story-like test
(render + a11y check) when it lands, and its licence noted in `web/THIRD_PARTY.md`.

## Editor: Monaco, the editor inside VS Code

**Choice.** Monaco is VS Code's editor component, extracted: the same keybindings, multi-cursor, find/replace,
command palette (F1), minimap, folding, diff editor, IntelliSense UI. Paired with **monaco-yaml**, which runs
`yaml-language-server` (the engine of Red Hat's VS Code YAML extension) in a web worker, it gives VS Code's YAML
experience: schema completion, hover docs, validation squiggles, formatting.

| Option | Verdict |
|---|---|
| **Monaco + monaco-yaml** | **Chosen.** VS Code's editing core and YAML language server, in-page, ~3-5 MB of JS lazy-loaded only on editor screens. |
| `@codingame/monaco-vscode-api` (VS Code's workbench services on top of Monaco: file system providers, themes, extensions, textmate) | **Escalation path** if we need VS Code features Monaco lacks (real extensions, workbench views). Heavier; not needed for manifests. |
| code-server / openvscode-server (full VS Code in an iframe) | Rejected: a second server process of 300-500 MB per instance, its own auth and file system, on a RAM-bound host. |
| CodeMirror 6 (`@uiw/react-codemirror`) | Lighter and better on mobile, but not VS Code: weaker YAML schema tooling. Kept as the fallback for small inline snippets if bundle size bites. |

**Integration (what we build around it).**

- **Self-hosted**, no CDN: `loader.config({ monaco })` with workers from Vite's `?worker` imports (editor worker,
  yaml worker from monaco-yaml, json worker).
- **Kubernetes schemas from the live cluster**: the `schemas` module turns the cluster's `/openapi/v3` into JSON
  Schemas per kind (cached per server version, CRDs included: Flux, Skupper, Traefik, Gateway API), served at
  `/api/v1/schemas/...`; monaco-yaml maps a document to the schema by its `apiVersion`/`kind`. Completion and
  validation therefore match the exact Kubernetes version the learner is talking to.
- **Workspace model**: the editor edits files of the attempt's workspace through the workspace API (load on open,
  save on Ctrl+S and debounced autosave); the terminal's shell runs in the same directory, so `kubectl apply -f
  web.yaml` in the terminal sees what the editor saved, and file changes made in the terminal (`kubectl get -o yaml
  > x.yaml`) reach open tabs through `workspace` SSE events (dirty tabs get a "file changed on disk" bar, as in VS Code).
- **Tabs and explorer** (VS Code layout): react-arborist explorer on the left, tab strip, breadcrumbs, the editor;
  dirty markers, close/close others, reopen closed.
- **Actions** bound to keys and the command palette: Save, Format, **Validate** (schema + server dry-run →
  Monaco markers from the diagnostics), **Apply to cluster**, **Diff with cluster** (fetch the live object; Monaco
  diff editor), **Diff with solution** (challenges, after a pass).
- Theme: a Monaco theme derived from our tokens (light and dark), Shiki uses the same colours.
- Optional later: `monaco-vim` toggle; inline AI is out of scope.

## Terminal

- xterm.js with `fit` (resize to pane), `web-links`, `search` (Ctrl+F in the terminal), `webgl` renderer with canvas
  fallback; theme from tokens.
- One WebSocket per terminal tab to `/ws/terminal/{session}`; sessions belong to an attempt (cwd = workspace,
  `opsforge` function and `${ns}`-aware prompt in the rcfile); several tabs allowed (like VS Code's terminal list).
- Command blocks in lectures/labs: **Run** (types + Enter, Killercoda), **Paste** (types, bracketed paste, no
  Enter), **Copy**.
- Reconnect: the server keeps a pty alive 60 s after a disconnect and replays its last 64 KB of output.

## Structure

```
web/
  src/
    app/                 providers (Query, Router, theme), error boundary, SSE client
    routes/              TanStack file routes: index, courses/$course, learn/$course/$item, badges, settings
    features/
      catalog/           course cards, filters, continue-learning row
      course/            course page: header, outcomes, curriculum accordion, roadmap
      player/            shell (top bar, sidebar, prev/next), item router
      lecture/           Markdown renderer, notes, resources
      quiz/              question view, navigator, results, timer
      lab/               steps, check results, hints, reset; uses terminal + labmap
      challenge/         problem, editor workspace, run/submit, test cases, submissions, solution diff
      editor/            Monaco setup, workspace model, explorer, tabs, actions, schemas
      terminal/          xterm hook, sessions, tabs
      labmap/            mimic panel (SVG) + Animated Beam overlay
      rewards/           XP, badges, completion
    components/ui/       shadcn + registry components (owned copies)
    api/                 generated schema.d.ts, client, query keys, SSE event types
    lib/                 tokens, formatting, keyboard shortcuts
    test/                MSW handlers (generated + overrides), fixtures, render helpers
  e2e/                   Playwright specs (against academy with the fake lab controller)
```

Feature folders import `components/ui`, `api` and `lib`, never each other's internals (an `index.ts` per feature is
its public surface; ESLint `import/no-restricted-paths` enforces it). `editor`, `terminal` and `labmap` are leaf
features used by `lab` and `challenge`.

## Theming

shadcn's CSS variables are set from the Phase 10 tokens (`--background` = paper, `--card` = enamel, `--foreground`
= ink, `--primary` = ink, `--destructive` = signal red, `--chart-*` from lamps), light and dark ("night shift")
themes via a `data-theme` attribute with system default. Lamp colours stay the only accents; borrowed components
are restyled to them (no purple gradients from demos).

## Performance budgets

- Initial route JS ≤ 250 KB gzip; Monaco, Mermaid and xterm load lazily on the screens that use them.
- Lighthouse (local build) ≥ 90 performance and 100 accessibility on catalogue and course page.
- Editor ready ≤ 1.5 s after opening a challenge on this host.
