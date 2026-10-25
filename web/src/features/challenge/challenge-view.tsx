import { Check, Loader2, Lock, Play, RotateCcw, Send, X } from "lucide-react";
import { type ReactNode, useState } from "react";

import type { Item } from "@/api/client";
import { DifficultyLabel } from "@/components/difficulty";
import { Lamp } from "@/components/lamp";
import { Button } from "@/components/ui/button";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CodeEditor, FileTabs } from "@/features/editor";
import { Markdown } from "@/features/lecture";
import { useIsMobile } from "@/hooks/use-mobile";
import { duration } from "@/lib/format";
import { cn } from "@/lib/utils";
import { type Submission, submissions as past } from "@/mock";

type Challenge = NonNullable<Item["challenge"]>;
type Outcome =
  | { kind: "run"; ok: boolean; lines: string[] }
  | {
      kind: "submit";
      verdict: Submission["verdict"];
      cases: { name: string; ok: boolean; detail: string }[];
      runtime?: string;
    };

const verdictLamp = {
  Accepted: "green",
  "Wrong answer": "red",
  "Invalid manifest": "red",
  "Time limit exceeded": "amber",
} as const;

/** A coding challenge in the player (design.md §5b, LeetCode's problem screen): the statement, solution and
 * submissions left; the manifest editor right, with test cases and the result below it. Run = server dry-run,
 * Submit = apply in a scratch namespace and run the tests; both answer from mock data until the challenge module
 * exists. */
export function ChallengeView({ item, number, next }: { item: Item; number: string; next?: ReactNode }) {
  const c = item.challenge;
  const mobile = useIsMobile();
  const [files, setFiles] = useState<Record<string, string>>(() => ({ ...(c?.starter ?? {}) }));
  const [active, setActive] = useState(c?.files[0] ?? "");
  const [busy, setBusy] = useState<"run" | "submit" | null>(null);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [pane, setPane] = useState("cases");
  const [left, setLeft] = useState("description");
  const [history, setHistory] = useState<Submission[]>(past);
  if (!c) return null;
  const challenge: Challenge = c;
  const dirty = new Set(challenge.files.filter((f) => files[f] !== challenge.starter[f]));
  const accepted = history.some((h) => h.verdict === "Accepted");

  function act(kind: "run" | "submit") {
    setBusy(kind);
    setPane("result");
    setTimeout(() => {
      const text = Object.values(files).join("\n");
      const valid = /kind:\s*\S+/.test(text) && /name:\s*\S+/.test(text);
      const changed = dirty.size > 0;
      if (kind === "run") {
        setOutcome({
          kind: "run",
          ok: valid,
          lines: valid
            ? challenge.files.map((f) => `${f}: valid, accepted by the API server (dry run)`)
            : ["error: every document needs a kind and a metadata.name"],
        });
      } else {
        const verdict: Submission["verdict"] = !valid ? "Invalid manifest" : changed ? "Accepted" : "Wrong answer";
        const cases = challenge.tests.map((t, i) => ({
          name: t,
          ok: verdict === "Accepted" || (verdict === "Wrong answer" && i < challenge.tests.length - 1),
          detail: verdict === "Accepted" ? "passed" : "wanted 1 ready pod, found 0 (no container in the spec)",
        }));
        const runtime = verdict === "Accepted" ? "5 s" : undefined;
        setOutcome({ kind: "submit", verdict, cases, ...(runtime ? { runtime } : {}) });
        setHistory((h) => [...h, { at: new Date().toISOString(), verdict, ...(runtime ? { runtime } : {}) }]);
      }
      setBusy(null);
    }, 1100);
  }

  const description = (
    <Tabs value={left} onValueChange={setLeft} className="flex h-full flex-col gap-0 bg-paper">
      <TabsList variant="line" className="h-10 w-full justify-start rounded-none border-b-2 border-ink bg-enamel px-1">
        <TabsTrigger value="description" className="flex-none">
          Description
        </TabsTrigger>
        <TabsTrigger value="solution" className="flex-none">
          {accepted ? null : <Lock aria-hidden />} Solution
        </TabsTrigger>
        <TabsTrigger value="submissions" className="flex-none">
          Submissions ({history.length})
        </TabsTrigger>
      </TabsList>
      <TabsContent value="description" className="min-h-0 overflow-auto p-5 sm:p-6">
        <p className="text-ink-soft">
          Challenge {number}, {duration(item.minutes)}
        </p>
        <h1 id="item-title" className="mt-1 font-serif text-3xl leading-tight">
          {item.title}
        </h1>
        <p className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
          <DifficultyLabel level={challenge.difficulty} />
          <span className="text-ink-soft">{challenge.xp} XP</span>
          <span className="text-ink-soft">Ready within {duration(Math.ceil(challenge.time_limit_s / 60))}</span>
        </p>
        <Markdown className="mt-5">{challenge.statement}</Markdown>
        <h2 className="mt-6 font-extrabold">What the tests check</h2>
        <ol className="mt-2 list-decimal space-y-1 pl-6">
          {challenge.tests.map((t) => (
            <li key={t}>{t}</li>
          ))}
        </ol>
        <h2 className="mt-6 font-extrabold">Constraints</h2>
        <ul className="mt-2 list-disc space-y-1 pl-6 text-ink-soft">
          <li>Your files are applied into a fresh namespace, deleted afterwards.</li>
          <li>Requests at most 50m CPU and 32Mi memory per container.</li>
        </ul>
      </TabsContent>
      <TabsContent value="solution" className="min-h-0 overflow-auto p-5 sm:p-6">
        {accepted ? (
          <p>The reference manifest and why it works, with a diff against yours.</p>
        ) : (
          <div className="grid place-items-center gap-3 rounded-md border-2 border-dashed border-rule p-8 text-center">
            <Lock aria-hidden className="size-6 text-ink-soft" />
            <p className="font-semibold">Opens once your submission is accepted</p>
            <p className="max-w-[40ch] text-sm text-ink-soft">
              Then you can compare the reference manifest with yours, line by line.
            </p>
          </div>
        )}
      </TabsContent>
      <TabsContent value="submissions" className="min-h-0 overflow-auto">
        <table className="w-full text-sm">
          <thead className="border-b border-rule text-left text-ink-soft">
            <tr>
              <th className="px-5 py-2 font-normal">Verdict</th>
              <th className="px-5 py-2 font-normal">Ready in</th>
              <th className="px-5 py-2 font-normal">When</th>
            </tr>
          </thead>
          <tbody>
            {[...history].reverse().map((h) => (
              <tr key={h.at} className="border-b border-rule">
                <td className="px-5 py-2.5">
                  <span
                    className={cn(
                      "flex items-center gap-2 font-semibold",
                      h.verdict === "Accepted" ? "text-green" : "text-red",
                    )}
                  >
                    <Lamp on={verdictLamp[h.verdict]} /> {h.verdict}
                  </span>
                </td>
                <td className="px-5 py-2.5">{h.runtime ?? "—"}</td>
                <td className="px-5 py-2.5 text-ink-soft">{when.format(new Date(h.at))}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </TabsContent>
    </Tabs>
  );

  const editor = (
    <div className="flex h-full flex-col">
      <FileTabs files={challenge.files} active={active} dirty={dirty} onPick={setActive}>
        <span>YAML</span>
        <Button
          variant="ghost"
          size="xs"
          onClick={() => {
            setFiles({ ...challenge.starter });
          }}
          disabled={dirty.size === 0}
        >
          <RotateCcw aria-hidden /> Reset to starter
        </Button>
      </FileTabs>
      <CodeEditor
        label={`Edit ${active}`}
        value={files[active] ?? ""}
        onChange={(v) => {
          setFiles({ ...files, [active]: v });
        }}
        className="flex-1"
      />
    </div>
  );

  const results = (
    <Tabs value={pane} onValueChange={setPane} className="flex h-full flex-col gap-0 bg-paper">
      <div className="flex items-center justify-between border-b-2 border-ink bg-enamel pr-2">
        <TabsList variant="line" className="h-10 rounded-none bg-transparent px-1">
          <TabsTrigger value="cases">Test cases</TabsTrigger>
          <TabsTrigger value="result">Result</TabsTrigger>
        </TabsList>
        <span className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            className="bg-paper"
            disabled={busy !== null}
            onClick={() => {
              act("run");
            }}
          >
            {busy === "run" ? (
              <Loader2 aria-hidden className="animate-spin motion-reduce:animate-none" />
            ) : (
              <Play aria-hidden />
            )}
            Run
          </Button>
          <Button
            size="sm"
            disabled={busy !== null}
            onClick={() => {
              act("submit");
            }}
          >
            {busy === "submit" ? (
              <Loader2 aria-hidden className="animate-spin motion-reduce:animate-none" />
            ) : (
              <Send aria-hidden />
            )}
            Submit
          </Button>
        </span>
      </div>
      <TabsContent value="cases" className="min-h-0 overflow-auto p-4">
        <ol className="space-y-2">
          {challenge.tests.map((t, i) => (
            <li key={t} className="rounded-md border border-rule bg-enamel/40 px-3 py-2 text-sm">
              <span className="font-semibold">Case {i + 1}</span> <span className="text-ink-soft">{t}</span>
            </li>
          ))}
        </ol>
      </TabsContent>
      <TabsContent value="result" className="min-h-0 overflow-auto p-4" aria-live="polite">
        {busy ? (
          <p className="flex items-center gap-2 text-ink-soft">
            <Loader2 aria-hidden className="size-4 animate-spin motion-reduce:animate-none" />
            {busy === "run"
              ? "Validating against the API server…"
              : "Applying into a scratch namespace and running the tests…"}
          </p>
        ) : !outcome ? (
          <p className="text-ink-soft">
            Run validates your files without touching the cluster. Submit applies them and runs the tests.
          </p>
        ) : outcome.kind === "run" ? (
          <div>
            <p className={cn("text-lg font-extrabold", outcome.ok ? "text-green" : "text-red")}>
              {outcome.ok ? "Valid manifest" : "Invalid manifest"}
            </p>
            <pre className="mt-2 overflow-x-auto rounded-md bg-term-bg p-3 font-mono text-xs leading-5 text-term-fg">
              {outcome.lines.join("\n")}
            </pre>
          </div>
        ) : (
          <div>
            <p className={cn("text-xl font-extrabold", outcome.verdict === "Accepted" ? "text-green" : "text-red")}>
              <span>{outcome.verdict}</span>
              {outcome.runtime ? (
                <span className="ml-3 text-sm font-normal text-ink-soft">Ready in {outcome.runtime}</span>
              ) : null}
            </p>
            <ol className="mt-3 space-y-1.5">
              {outcome.cases.map((k, i) => (
                <li key={k.name} className="flex gap-2 text-sm">
                  {k.ok ? (
                    <Check aria-label="Passed" className="size-4 shrink-0 text-green" strokeWidth={3} />
                  ) : (
                    <X aria-label="Failed" className="size-4 shrink-0 text-red" strokeWidth={3} />
                  )}
                  <span>
                    <span className="font-semibold">Case {i + 1}</span> {k.name}
                    {k.ok ? null : <span className="block font-mono text-xs text-red">{k.detail}</span>}
                  </span>
                </li>
              ))}
            </ol>
            {outcome.verdict === "Accepted" ? <div className="mt-4 flex gap-3">{next}</div> : null}
          </div>
        )}
      </TabsContent>
    </Tabs>
  );

  return (
    <section aria-labelledby="item-title" className="h-full">
      <ResizablePanelGroup orientation={mobile ? "vertical" : "horizontal"} className="h-full">
        <ResizablePanel defaultSize="42%" minSize="25%">
          {description}
        </ResizablePanel>
        <ResizableHandle withHandle className="bg-ink" />
        <ResizablePanel defaultSize="58%" minSize="30%">
          <ResizablePanelGroup orientation="vertical">
            <ResizablePanel defaultSize="62%" minSize="25%">
              {editor}
            </ResizablePanel>
            <ResizableHandle withHandle className="bg-ink" />
            <ResizablePanel defaultSize="38%" minSize="15%">
              {results}
            </ResizablePanel>
          </ResizablePanelGroup>
        </ResizablePanel>
      </ResizablePanelGroup>
    </section>
  );
}

const when = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" });
