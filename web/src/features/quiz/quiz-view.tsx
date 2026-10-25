import { Link } from "@tanstack/react-router";
import { Check, ChevronLeft, ChevronRight, Clock, RotateCcw, X } from "lucide-react";
import { useEffect, useState } from "react";

import type { Item } from "@/api/client";
import { Lamp } from "@/components/lamp";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Progress } from "@/components/ui/progress";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { useIsMobile } from "@/hooks/use-mobile";
import { duration } from "@/lib/format";
import { cn } from "@/lib/utils";
import { type MockQuestion, quizQuestions } from "@/mock";

type Answers = Record<string, number[]>;
type Phase = { at: "intro" } | { at: "taking"; started: number } | { at: "results"; answers: Answers };

/** A quiz in the player (design.md §4): Udemy's flow (intro, one question at a time, results) on LeetCode's split
 * (the question left, your answer right, a navigator and Submit below). */
export function QuizView({ item, number, next }: { item: Item; number: string; next?: React.ReactNode }) {
  const [phase, setPhase] = useState<Phase>({ at: "intro" });
  const quiz = item.quiz;
  if (!quiz) return null;

  if (phase.at === "intro")
    return (
      <section aria-labelledby="item-title" className="mx-auto max-w-[70ch]">
        <p className="text-ink-soft">
          Quiz {number}, {duration(item.minutes)}
        </p>
        <h1 id="item-title" className="mt-1 font-serif text-3xl leading-tight sm:text-4xl">
          {item.title}
        </h1>
        <dl className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
          {[
            ["Questions", String(quizQuestions.length)],
            ["To pass", `${String(quiz.pass)} %`],
            ["Time", quiz.time_limit_s ? duration(Math.ceil(quiz.time_limit_s / 60)) : "No limit"],
            ["XP", String(quiz.xp)],
          ].map(([term, value]) => (
            <div key={term} className="rounded-md border-2 border-ink p-3">
              <dt className="text-sm text-ink-soft">{term}</dt>
              <dd className="text-2xl font-extrabold">{value}</dd>
            </div>
          ))}
        </dl>
        <p className="mt-6 text-ink-soft">
          One question at a time; skip any and come back before you submit. Questions and choices are shuffled on every
          attempt, and the XP comes with the first pass.
        </p>
        <div className="mt-4 rounded-md border-2 border-dashed border-rule p-4">
          <p className="font-semibold">Your attempts</p>
          <p className="text-sm text-ink-soft">Best 5 of 8 yesterday, not passed yet. Your best score is kept.</p>
        </div>
        <Button
          size="lg"
          className="mt-6"
          onClick={() => {
            setPhase({ at: "taking", started: Date.now() });
          }}
        >
          Start quiz
        </Button>
      </section>
    );

  if (phase.at === "taking")
    return (
      <Taking
        title={item.title}
        number={number}
        limit={quiz.time_limit_s ?? undefined}
        started={phase.started}
        onSubmit={(answers) => {
          setPhase({ at: "results", answers });
        }}
      />
    );

  return (
    <Results
      title={item.title}
      pass={quiz.pass}
      xp={quiz.xp}
      answers={phase.answers}
      onRetake={() => {
        setPhase({ at: "taking", started: Date.now() });
      }}
      next={next}
    />
  );
}

function Taking({
  title,
  number,
  limit,
  started,
  onSubmit,
}: {
  title: string;
  number: string;
  limit?: number | undefined;
  started: number;
  onSubmit: (a: Answers) => void;
}) {
  const [at, setAt] = useState(0);
  const [answers, setAnswers] = useState<Answers>({});
  const mobile = useIsMobile();
  const q = quizQuestions[at];
  if (!q) return null;
  const answered = Object.values(answers).filter((a) => a.length).length;
  const last = at === quizQuestions.length - 1;

  return (
    <section aria-labelledby="quiz-title" className="mx-auto max-w-6xl">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 id="quiz-title" className="font-serif text-2xl">
          Quiz {number}: {title}
        </h1>
        {limit ? <Countdown limit={limit} started={started} /> : null}
      </div>
      <div className="mt-4 overflow-hidden rounded-md border-2 border-ink">
        <ResizablePanelGroup orientation={mobile ? "vertical" : "horizontal"} className="min-h-[28rem]">
          <ResizablePanel defaultSize="55%" minSize="30%" className="bg-paper">
            <div className="h-full overflow-auto p-5 sm:p-6">
              <p className="text-sm text-ink-soft">
                Question {at + 1} of {quizQuestions.length}
              </p>
              <h2 className="mt-2 font-serif text-xl leading-snug sm:text-2xl">{q.prompt}</h2>
              {q.output ? (
                <pre className="mt-5 overflow-x-auto rounded-md bg-term-bg p-4 font-mono text-[0.8rem] leading-6 text-term-fg">
                  {q.output}
                </pre>
              ) : null}
            </div>
          </ResizablePanel>
          <ResizableHandle withHandle className="bg-ink" />
          <ResizablePanel defaultSize="45%" minSize="30%" className="bg-enamel">
            <div className="flex h-full flex-col p-5 sm:p-6">
              <p id="answer-label" className="font-semibold">
                {q.kind === "multiple" ? "Select all that apply" : "Choose one"}
              </p>
              <Choices
                q={q}
                value={answers[q.id] ?? []}
                onChange={(v) => {
                  setAnswers({ ...answers, [q.id]: v });
                }}
              />
              <div className="mt-auto flex justify-between gap-2 pt-6">
                <Button
                  variant="outline"
                  className="bg-paper"
                  disabled={at === 0}
                  onClick={() => {
                    setAt(at - 1);
                  }}
                >
                  <ChevronLeft aria-hidden /> Previous
                </Button>
                {last ? null : (
                  <Button
                    onClick={() => {
                      setAt(at + 1);
                    }}
                  >
                    {answers[q.id]?.length ? "Next" : "Skip"} <ChevronRight aria-hidden />
                  </Button>
                )}
              </div>
            </div>
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-4">
        <ol aria-label="Questions" className="flex flex-wrap gap-2">
          {quizQuestions.map((qq, i) => {
            const done = Boolean(answers[qq.id]?.length);
            return (
              <li key={qq.id}>
                <button
                  type="button"
                  aria-current={i === at ? "step" : undefined}
                  aria-label={`Question ${String(i + 1)}, ${done ? "answered" : "not answered"}`}
                  onClick={() => {
                    setAt(i);
                  }}
                  className={cn(
                    "grid size-9 place-items-center rounded-md border-2 border-ink text-sm font-extrabold",
                    done ? "bg-ink text-paper" : "bg-paper",
                    i === at && "ring-3 ring-amber ring-offset-2 ring-offset-paper",
                  )}
                >
                  {i + 1}
                </button>
              </li>
            );
          })}
        </ol>
        <div className="flex items-center gap-4">
          <span className="text-sm text-ink-soft">
            {answered} of {quizQuestions.length} answered
          </span>
          <Button
            size="lg"
            onClick={() => {
              onSubmit(answers);
            }}
          >
            Submit quiz
          </Button>
        </div>
      </div>
    </section>
  );
}

function Choices({ q, value, onChange }: { q: MockQuestion; value: number[]; onChange: (v: number[]) => void }) {
  const box = (on: boolean) =>
    cn(
      "flex cursor-pointer items-start gap-3 rounded-md border-2 bg-paper p-3 leading-snug",
      on ? "border-ink shadow-[3px_3px_0_var(--ink)]" : "border-ink/25 hover:border-ink/60",
    );
  if (q.kind === "single")
    return (
      <RadioGroup
        aria-labelledby="answer-label"
        value={value[0] === undefined ? "" : String(value[0])}
        onValueChange={(v) => {
          onChange([Number(v)]);
        }}
        className="mt-3 gap-2.5"
      >
        {q.choices.map((c, i) => (
          <label key={c} className={box(value.includes(i))}>
            <RadioGroupItem value={String(i)} className="mt-0.5 border-ink" />
            {c}
          </label>
        ))}
      </RadioGroup>
    );
  return (
    <div role="group" aria-labelledby="answer-label" className="mt-3 grid gap-2.5">
      {q.choices.map((c, i) => (
        <label key={c} className={box(value.includes(i))}>
          <Checkbox
            checked={value.includes(i)}
            onCheckedChange={(on) => {
              onChange(on ? [...value, i].sort() : value.filter((v) => v !== i));
            }}
            className="mt-0.5 border-ink"
          />
          {c}
        </label>
      ))}
    </div>
  );
}

function Countdown({ limit, started }: { limit: number; started: number }) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const t = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => {
      clearInterval(t);
    };
  }, []);
  const left = Math.max(0, limit - Math.floor((now - started) / 1000));
  const low = left < 60;
  return (
    <p role="timer" aria-live={low ? "polite" : "off"} className="flex items-center gap-2 font-semibold tabular-nums">
      <Clock aria-hidden className={cn("size-4", low && "text-red")} />
      {Math.floor(left / 60)}:{String(left % 60).padStart(2, "0")} left
    </p>
  );
}

function same(a: number[] = [], b: number[]) {
  return a.length === b.length && a.every((v, i) => v === b[i]);
}

function Results({
  title,
  pass,
  xp,
  answers,
  onRetake,
  next,
}: {
  title: string;
  pass: number;
  xp: number;
  answers: Answers;
  onRetake: () => void;
  next?: React.ReactNode;
}) {
  const right = quizQuestions.filter((q) => same(answers[q.id], q.answer)).length;
  const pct = Math.round((right / quizQuestions.length) * 100);
  const passed = pct >= pass;
  return (
    <section aria-labelledby="result" className="mx-auto max-w-[75ch]">
      <div className={cn("rounded-md border-2 border-ink p-5", passed ? "bg-enamel" : "bg-paper")}>
        <p className="flex items-center gap-2 text-ink-soft">
          <Lamp on={passed ? "green" : "red"} /> {title}
        </p>
        <h1 id="result" className="mt-1 font-serif text-3xl">
          {passed ? "Passed" : "Not passed yet"}: {right} of {quizQuestions.length}
        </h1>
        <Progress
          value={pct}
          className="mt-3"
          aria-label={`Score ${String(pct)} %`}
          indicatorClassName={passed ? "" : "bg-red"}
        />
        <p className="mt-2 text-sm text-ink-soft">
          {`${String(pct)} %, ${String(pass)} % to pass. `}
          {passed ? `+${String(xp)} XP, your first pass.` : "Read the explanations, then try again."}
        </p>
        <div className="mt-4 flex flex-wrap gap-3">
          <Button variant="outline" className="bg-paper" onClick={onRetake}>
            <RotateCcw aria-hidden /> Retake quiz
          </Button>
          {next}
        </div>
      </div>

      <ol className="mt-8 space-y-4">
        {quizQuestions.map((q, i) => {
          const mine = answers[q.id] ?? [];
          const ok = same(mine, q.answer);
          return (
            <li key={q.id} className="rounded-md border-2 border-ink/30 p-4">
              <p className="flex gap-3">
                {ok ? (
                  <Check aria-label="Right" className="mt-0.5 size-5 shrink-0 text-green" strokeWidth={3} />
                ) : (
                  <X aria-label="Wrong" className="mt-0.5 size-5 shrink-0 text-red" strokeWidth={3} />
                )}
                <span className="font-semibold">
                  {i + 1}. {q.prompt}
                </span>
              </p>
              <dl className="mt-2 space-y-1 pl-8 text-sm">
                <div>
                  <dt className="inline text-ink-soft">You chose: </dt>
                  <dd className="inline">
                    {mine.length ? mine.map((m) => q.choices[m]).join(", ") : "nothing (skipped)"}
                  </dd>
                </div>
                {ok ? null : (
                  <div>
                    <dt className="inline text-ink-soft">Answer: </dt>
                    <dd className="inline font-semibold">{q.answer.map((m) => q.choices[m]).join(", ")}</dd>
                  </div>
                )}
              </dl>
              <p className="mt-2 pl-8 font-serif leading-relaxed">{q.explain}</p>
              {q.see && !ok ? (
                <p className="mt-1 pl-8 text-sm">
                  <Link to="." className="underline underline-offset-4">
                    Review {q.see}
                  </Link>
                </p>
              ) : null}
            </li>
          );
        })}
      </ol>
    </section>
  );
}
