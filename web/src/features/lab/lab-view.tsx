import { Check, ChevronDown, Lightbulb, Loader2, Play, RotateCcw, Square } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";

import type { Item } from "@/api/client";
import { Lamp, type LampColour } from "@/components/lamp";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/components/ui/dialog";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CodeEditor, FileTabs } from "@/features/editor";
import { LabMap } from "@/features/labmap";
import { Markdown } from "@/features/lecture";
import { TerminalPane } from "@/features/terminal";
import { useIsMobile } from "@/hooks/use-mobile";
import { count, duration } from "@/lib/format";
import { cn } from "@/lib/utils";
import { setupLog, terminalTranscript } from "@/mock";

type Status = "idle" | "setup" | "running" | "passed";
type Lab = NonNullable<Item["lab"]>;
type Step = Lab["steps"][number];

interface Checked {
  ok: boolean;
  rows: { what: string; wanted: string; found: string; ok: boolean }[];
}

const statusName: Record<Status, string> = {
  idle: "Not started",
  setup: "Setting up",
  running: "Running",
  passed: "Passed",
};
const statusLamp: Record<Status, LampColour> = { idle: "off", setup: "amber", running: "green", passed: "green" };

/** A lab in the player (design.md §5, Killercoda's layout): the steps left, each checked on the lab before the next
 * opens; the terminal, the lab's files and the lab map right. The session runs on mock data until labsession is
 * hooked up (start = 202 + SSE, check = an operation). */
export function LabView({ item, number, next }: { item: Item; number: string; next?: ReactNode }) {
  const lab = item.lab;
  const mobile = useIsMobile();
  const [status, setStatus] = useState<Status>("idle");
  const [log, setLog] = useState<string[]>([]);
  const [at, setAt] = useState(0);
  const [hints, setHints] = useState<Record<string, number>>({});
  const [results, setResults] = useState<Record<string, Checked>>({});
  const [checking, setChecking] = useState(false);
  const [tab, setTab] = useState("terminal");
  const [confirmAbandon, setConfirmAbandon] = useState(false);

  // Setup streams its log as the operation's SSE events will
  useEffect(() => {
    if (status !== "setup") return;
    const timer = setTimeout(() => {
      const n = log.length + 1;
      setLog(setupLog.slice(0, n));
      if (n >= setupLog.length) setStatus("running");
    }, 450);
    return () => {
      clearTimeout(timer);
    };
  }, [status, log.length]);

  if (!lab) return null;
  const total = lab.steps.length;
  const used = Object.values(hints).reduce((n, h) => n + h, 0);
  const xp = Math.round(lab.xp * Math.max(0, 1 - 0.1 * used));

  function start() {
    setLog([]);
    setAt(0);
    setResults({});
    setHints({});
    setStatus("setup");
  }

  function check(step: Step) {
    setChecking(true);
    setTimeout(() => {
      const tries = results[step.id] ? 2 : 1;
      const ok = step.check.type !== "probe" || tries > 1;
      setResults((r) => ({
        ...r,
        [step.id]: {
          ok,
          rows:
            step.check.type === "probe"
              ? [
                  { what: "Nodes Ready", wanted: "4", found: "4", ok: true },
                  { what: "Label region on c-w1", wanted: "c", found: ok ? "c" : "(none)", ok },
                ]
              : [],
        },
      }));
      setChecking(false);
      if (ok) {
        if (at === total - 1) setStatus("passed");
        else setAt(at + 1);
      }
    }, 900);
  }

  const steps = (
    <div className="h-full overflow-auto bg-surface">
      <div className="p-5 sm:p-6">
        <p className="text-ink-soft">
          Lab {number}, {duration(item.minutes)}
        </p>
        <h1 id="item-title" className="mt-1 text-2xl font-semibold tracking-[-0.01em]">
          {item.title}
        </h1>
        <p className="mt-2 text-ink-soft">{lab.summary}</p>
        <details className="group mt-4 rounded-lg border border-rule/30" open={status === "idle"}>
          <summary className="flex cursor-pointer items-center justify-between px-4 py-2 font-semibold">
            Briefing
            <ChevronDown aria-hidden className="size-4 transition-transform group-open:rotate-180" />
          </summary>
          <Markdown className="px-4 pb-3">{lab.briefing}</Markdown>
        </details>

        {status === "passed" ? (
          <Debrief xp={xp} hints={used} next={next} onAgain={start} debrief={lab.debrief} />
        ) : (
          <ol aria-label="Steps" className="mt-6">
            {lab.steps.map((s, i) => (
              <StepRow
                key={s.id}
                step={s}
                index={i}
                last={i === lab.steps.length - 1}
                state={i < at ? "passed" : i === at ? "current" : "waiting"}
                running={status === "running"}
                hints={hints[s.id] ?? 0}
                result={results[s.id]}
                checking={checking && i === at}
                onHint={() => {
                  setHints({ ...hints, [s.id]: (hints[s.id] ?? 0) + 1 });
                }}
                onCheck={() => {
                  check(s);
                }}
                penalty={Math.round(lab.xp * 0.1)}
              />
            ))}
          </ol>
        )}
      </div>
    </div>
  );

  const side = (
    <Tabs value={tab} onValueChange={setTab} className="flex h-full flex-col gap-0 bg-term-bg">
      <div className="flex items-center justify-between gap-2 border-b border-rule bg-enamel pr-3">
        <TabsList variant="line" className="h-10 rounded-none bg-transparent px-1">
          <TabsTrigger value="terminal">Terminal</TabsTrigger>
          <TabsTrigger value="files">Files</TabsTrigger>
          <TabsTrigger value="map" className="gap-2">
            Lab map <Lamp on="amber" />
            <span className="sr-only">(a link changed)</span>
          </TabsTrigger>
        </TabsList>
        <span className="flex items-center gap-2 text-sm text-ink-soft">
          <Lamp on={status === "running" ? "green" : "off"} />
          {status === "running" ? "Connected" : "No session"}
        </span>
      </div>
      <TabsContent value="terminal" className="relative min-h-0">
        {status === "running" || status === "passed" ? (
          <TerminalPane output={terminalTranscript} className="absolute inset-0" />
        ) : (
          <StartPanel lab={lab} status={status} log={log} onStart={start} />
        )}
      </TabsContent>
      <TabsContent value="files" className="min-h-0">
        <Files files={lab.files} />
      </TabsContent>
      <TabsContent value="map" className="min-h-0 overflow-auto bg-surface p-5">
        <LabMap />
      </TabsContent>
    </Tabs>
  );

  return (
    <section aria-labelledby="item-title" className="flex h-full flex-col">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-b border-rule bg-surface px-4 py-2 text-sm">
        <span className="flex items-center gap-2 font-semibold">
          <Lamp on={statusLamp[status]} />
          {statusName[status]}
        </span>
        <span className="text-ink-soft">
          {status === "running"
            ? `Step ${String(at + 1)} of ${String(lab.steps.length)}`
            : count(lab.steps.length, "step")}
        </span>
        <span className="text-ink-soft">
          {xp} XP{used ? `, ${count(used, "hint")} used` : ""}
        </span>
        {status === "running" ? (
          <span className="ml-auto flex gap-2">
            <Button variant="outline" size="sm" className="bg-surface" onClick={start}>
              <RotateCcw aria-hidden /> Reset lab
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="border-red/40 bg-surface text-red hover:bg-red-soft hover:text-red"
              onClick={() => {
                setConfirmAbandon(true);
              }}
            >
              <Square aria-hidden /> Abandon
            </Button>
          </span>
        ) : null}
      </div>
      <ResizablePanelGroup orientation={mobile ? "vertical" : "horizontal"} className="min-h-0 flex-1">
        <ResizablePanel defaultSize={mobile ? "55%" : "40%"} minSize="25%">
          {steps}
        </ResizablePanel>
        <ResizableHandle withHandle className="bg-rule" />
        <ResizablePanel defaultSize={mobile ? "45%" : "60%"} minSize="25%">
          {side}
        </ResizablePanel>
      </ResizablePanelGroup>

      <Dialog open={confirmAbandon} onOpenChange={setConfirmAbandon}>
        <DialogContent className="border border-rule bg-surface">
          <DialogTitle className="text-xl font-semibold font-normal">Abandon this lab?</DialogTitle>
          <DialogDescription className="text-ink-soft">
            The lab is torn down and every link healed. Steps you passed are not kept; you can start again any time.
          </DialogDescription>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline" className="bg-surface">
                Keep going
              </Button>
            </DialogClose>
            <Button
              className="bg-red text-surface hover:bg-red/90"
              onClick={() => {
                setConfirmAbandon(false);
                setStatus("idle");
                setLog([]);
              }}
            >
              Abandon lab
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function StartPanel({ lab, status, log, onStart }: { lab: Lab; status: Status; log: string[]; onStart: () => void }) {
  return (
    <div className="absolute inset-0 grid place-items-center overflow-auto p-6 text-term-fg">
      <div className="w-full max-w-md">
        {status === "idle" ? (
          <>
            <h2 className="text-xl font-semibold">Start the lab on your cluster</h2>
            <p className="mt-2 text-sm opacity-80">
              Setup creates the namespace and applies the lab's starting state. Finishing or abandoning always tears it
              down and heals every link.
            </p>
            <ul className="mt-4 space-y-1 text-sm">
              {lab.needs.length ? (
                <li className="flex items-center gap-2">
                  <Lamp on="green" /> Needs {lab.needs.join(", ")}: your lab has it
                </li>
              ) : null}
              <li className="flex items-center gap-2">
                <Lamp on="green" /> Your lab is up, stretched layout
              </li>
            </ul>
            <Button size="lg" onClick={onStart} className="mt-6 bg-term-fg text-term-bg hover:bg-term-fg/90">
              <Play aria-hidden /> Start lab
            </Button>
          </>
        ) : (
          <>
            <h2 className="flex items-center gap-2 text-xl font-semibold">
              <Loader2 aria-hidden className="size-5 animate-spin motion-reduce:animate-none" /> Setting up
            </h2>
            <ol aria-live="polite" className="mt-4 space-y-1.5 font-mono text-sm">
              {log.map((line) => (
                <li key={line} className="flex items-center gap-2">
                  <Check aria-hidden className="size-4 text-green" /> {line}
                </li>
              ))}
            </ol>
          </>
        )}
      </div>
    </div>
  );
}

const hintText = [
  "kubectl can add a label as a column: look for -L in kubectl get --help.",
  "The region label is topology.kubernetes.io/region.",
];

const quizChoices = ["Region a", "Region b", "Region c"];

function StepRow({
  step,
  index,
  last,
  state,
  hints,
  result,
  checking,
  onHint,
  onCheck,
  penalty,
  running,
}: {
  running: boolean;
  step: Step;
  index: number;
  last: boolean;
  state: "passed" | "current" | "waiting";
  hints: number;
  result: Checked | undefined;
  checking: boolean;
  onHint: () => void;
  onCheck: () => void;
  penalty: number;
}) {
  const [choice, setChoice] = useState("");
  return (
    <li className="relative flex gap-4 pb-6">
      {last ? null : (
        <span
          aria-hidden
          className={cn("absolute top-8 bottom-0 left-[13px] w-0.5", state === "passed" ? "bg-green" : "bg-rule")}
        />
      )}
      <span
        className={cn(
          "relative z-10 grid size-7 shrink-0 place-items-center rounded-full border-2 text-xs font-semibold",
          state === "passed" && "border-green bg-green text-white",
          state === "current" && "station-current border-ember bg-ember text-white",
          state === "waiting" && "border-control bg-surface text-ink-soft",
        )}
      >
        {state === "passed" ? <Check aria-hidden className="size-3.5" strokeWidth={3.5} /> : index + 1}
      </span>
      <div className="min-w-0 flex-1">
        <h2 className={cn("pt-0.5 font-semibold", state === "waiting" && "text-ink-soft")}>
          {step.title}
          <span className="sr-only">, {state === "current" ? "current step" : state}</span>
        </h2>
        {state === "current" ? (
          <div className="mt-2">
            <Markdown>{step.body}</Markdown>
            {step.check.type === "quiz" ? (
              <RadioGroup value={choice} onValueChange={setChoice} aria-label="Your answer" className="mt-4 gap-2">
                {quizChoices.map((c, i) => (
                  <label key={c} className="flex items-center gap-2">
                    <RadioGroupItem value={String(i)} className="border-control" /> {c}
                  </label>
                ))}
              </RadioGroup>
            ) : null}

            {step.hints ? (
              <div className="mt-4 space-y-2">
                {hintText.slice(0, hints).map((h, i) => (
                  <p key={h} className="flex gap-2 rounded-md border-l-4 border-amber bg-enamel/60 p-3 text-sm">
                    <Lightbulb aria-hidden className="size-4 shrink-0 text-amber" />
                    <span>
                      <span className="font-semibold">Hint {i + 1}: </span>
                      {h}
                    </span>
                  </p>
                ))}
                {hints < step.hints && running ? (
                  <Button variant="link" className="h-auto px-0 text-ink-soft" onClick={onHint}>
                    <Lightbulb aria-hidden /> Show hint {hints + 1} of {step.hints} (−{penalty} XP)
                  </Button>
                ) : null}
              </div>
            ) : null}

            {result && !result.ok ? <CheckResult result={result} /> : null}
            {running ? null : <p className="mt-4 text-sm text-ink-soft">Start the lab to check this step.</p>}

            <Button className="mt-4" onClick={onCheck} disabled={checking || (step.check.type === "quiz" && !choice)}>
              {checking ? <Loader2 aria-hidden className="animate-spin motion-reduce:animate-none" /> : null}
              {checking ? "Checking on the lab…" : step.check.type === "confirm" ? "I have done this" : "Check"}
            </Button>
          </div>
        ) : null}
      </div>
    </li>
  );
}

/** Phase 10's itemised result: each expectation with what was wanted and what the lab showed. */
function CheckResult({ result }: { result: Checked }) {
  return (
    <div role="alert" className="mt-4 overflow-hidden rounded-md border-2 border-red">
      <p className="bg-red px-3 py-1.5 text-sm font-semibold text-surface">Not yet: the lab does not match</p>
      <table className="w-full text-sm">
        <thead className="text-left text-ink-soft">
          <tr>
            <th className="px-3 py-1.5 font-normal">Check</th>
            <th className="px-3 py-1.5 font-normal">Wanted</th>
            <th className="px-3 py-1.5 font-normal">Found</th>
          </tr>
        </thead>
        <tbody className="font-mono">
          {result.rows.map((r) => (
            <tr key={r.what} className="border-t border-rule">
              <td className="px-3 py-1.5 font-sans">
                <span className="flex items-center gap-2">
                  <Lamp on={r.ok ? "green" : "red"} /> {r.what}
                </span>
              </td>
              <td className="px-3 py-1.5">{r.wanted}</td>
              <td className={cn("px-3 py-1.5", !r.ok && "font-semibold text-red")}>{r.found}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Debrief({
  xp,
  hints,
  next,
  onAgain,
  debrief,
}: {
  xp: number;
  hints: number;
  next?: ReactNode;
  onAgain: () => void;
  debrief: string;
}) {
  return (
    <div role="status" className="mt-6 rounded-lg border border-rule bg-enamel p-5">
      <p className="flex items-center gap-2 text-ink-soft">
        <Lamp on="green" /> Every step passed. The lab is torn down and the links healed.
      </p>
      <h2 className="mt-1 text-2xl font-semibold tracking-[-0.01em]">Lab passed</h2>
      <dl className="mt-4 grid grid-cols-3 gap-3">
        {[
          ["XP", `+${String(xp)}`],
          ["Time", "11 min"],
          ["Hints", String(hints)],
        ].map(([t, v]) => (
          <div key={t} className="rounded-lg border border-rule bg-surface p-3">
            <dt className="text-sm text-ink-soft">{t}</dt>
            <dd className="text-xl font-semibold">{v}</dd>
          </div>
        ))}
      </dl>
      {debrief ? <Markdown className="mt-4">{debrief}</Markdown> : null}
      <div className="mt-5 flex flex-wrap gap-3">
        {next}
        <Button variant="outline" className="bg-surface" onClick={onAgain}>
          <RotateCcw aria-hidden /> Run it again
        </Button>
      </div>
    </div>
  );
}

function Files({ files }: { files: string[] }) {
  const names = files.length ? files : ["notes.md"];
  const [active, setActive] = useState(names[0] ?? "");
  const [content, setContent] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      names.map((f) => [
        f,
        f.endsWith(".yaml")
          ? "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: hello\ndata:\n  greeting: hello from region a\n"
          : "",
      ]),
    ),
  );
  return (
    <div className="flex h-full flex-col">
      <FileTabs files={names} active={active} onPick={setActive}>
        Saved to the lab's folder
      </FileTabs>
      <CodeEditor
        label={`Edit ${active}`}
        value={content[active] ?? ""}
        onChange={(v) => {
          setContent({ ...content, [active]: v });
        }}
        className="flex-1"
      />
    </div>
  );
}
