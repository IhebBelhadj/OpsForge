import type { ReactNode } from "react";

import type { Item } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Markdown } from "@/features/lecture";
import { count, duration } from "@/lib/format";
import { cn } from "@/lib/utils";

// Read-only views of the items the player cannot run yet: what the item is and asks, without its answers (the API
// never sends them). Taking quizzes, running labs and submitting challenges replace these screens.

function Heading({ kind, number, item, children }: { kind: string; number: string; item: Item; children?: ReactNode }) {
  return (
    <header>
      <p className="text-sm font-semibold tracking-wide text-ink-soft uppercase">
        {kind} {number} · {duration(item.minutes)}
      </p>
      <h1 id="item-title" className="mt-1 font-serif text-3xl leading-tight sm:text-4xl">
        {item.title}
      </h1>
      {children}
    </header>
  );
}

function NotYet({ children }: { children: ReactNode }) {
  return (
    <p role="note" className="mt-6 rounded-md border-2 border-dashed border-rule p-4 text-ink-soft">
      {children}
    </p>
  );
}

export function QuizPreview({ item, number }: { item: Item; number: string }) {
  const q = item.quiz;
  if (!q) return null;
  return (
    <section aria-labelledby="item-title" className="mx-auto max-w-[70ch]">
      <Heading kind="Quiz" number={number} item={item} />
      <dl className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
        {[
          ["Questions", String(q.questions)],
          ["To pass", `${String(q.pass)} %`],
          ["Time", q.time_limit_s ? duration(Math.ceil(q.time_limit_s / 60)) : "No limit"],
          ["XP", String(q.xp)],
        ].map(([term, value]) => (
          <div key={term} className="rounded-md border-2 border-ink p-3">
            <dt className="text-sm text-ink-soft">{term}</dt>
            <dd className="text-2xl font-extrabold">{value}</dd>
          </div>
        ))}
      </dl>
      <NotYet>Taking quizzes in the player is not available yet.</NotYet>
    </section>
  );
}

const checkName = { probe: "Checked on the lab", confirm: "You confirm", quiz: "A question" } as const;

export function LabPreview({ item, number }: { item: Item; number: string }) {
  const lab = item.lab;
  if (!lab) return null;
  return (
    <section aria-labelledby="item-title" className="mx-auto max-w-[70ch]">
      <Heading kind="Lab" number={number} item={item}>
        <p className="mt-3 text-lg text-ink-soft">{lab.summary}</p>
        <p className="mt-2 text-sm">
          {count(lab.steps.length, "step")} · {String(lab.xp)} XP
          {lab.needs.length ? ` · needs ${lab.needs.join(", ")}` : ""}
        </p>
      </Heading>
      <Markdown className="mt-6">{lab.briefing}</Markdown>
      <ol className="mt-6 space-y-4">
        {lab.steps.map((s, i) => (
          <li key={s.id} className="rounded-md border-2 border-ink p-4">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <h2 className="font-extrabold">
                {i + 1} &nbsp;{s.title}
              </h2>
              <span className="text-sm text-ink-soft">
                {checkName[s.check.type]}
                {s.hints ? ` · ${count(s.hints, "hint")}` : ""}
              </span>
            </div>
            <Markdown className="mt-2">{s.body}</Markdown>
          </li>
        ))}
      </ol>
      <NotYet>
        Starting labs from the player is not available yet; they will run in a terminal beside these steps.
      </NotYet>
    </section>
  );
}

const difficulty = { easy: "border-green", medium: "border-amber", hard: "border-red" } as const;

export function ChallengePreview({ item, number }: { item: Item; number: string }) {
  const c = item.challenge;
  if (!c) return null;
  return (
    <section aria-labelledby="item-title" className="mx-auto max-w-[70ch]">
      <Heading kind="Challenge" number={number} item={item}>
        <p className="mt-3 flex flex-wrap items-center gap-2 text-sm">
          <Badge variant="outline" className={cn("border-2 capitalize", difficulty[c.difficulty])}>
            {c.difficulty}
          </Badge>
          {String(c.xp)} XP · time limit {duration(Math.ceil(c.time_limit_s / 60))}
        </p>
      </Heading>
      <Markdown className="mt-6">{c.statement}</Markdown>
      <h2 className="mt-8 text-lg font-extrabold">Test cases</h2>
      <ol className="mt-2 list-decimal pl-6">
        {c.tests.map((t) => (
          <li key={t}>{t}</li>
        ))}
      </ol>
      {c.files.map((f) => (
        <div key={f} className="mt-6">
          <h2 className="font-mono text-sm">{f}</h2>
          <Markdown>{`\`\`\`yaml\n${c.starter[f] ?? ""}\n\`\`\``}</Markdown>
        </div>
      ))}
      <NotYet>Solving challenges in the browser editor is not available yet.</NotYet>
    </section>
  );
}
