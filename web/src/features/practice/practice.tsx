import { Link, useNavigate } from "@tanstack/react-router";
import { CircleDashed, CircleCheck, Search, Shuffle } from "lucide-react";
import { useMemo, useState } from "react";

import { DifficultyLabel } from "@/components/difficulty";
import { ItemIcon } from "@/components/item-icon";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { typeName } from "@/lib/course";
import { duration } from "@/lib/format";
import { cn } from "@/lib/utils";
import { type Problem, problems, topics } from "@/mock";

import { SolvedRing } from "./solved-ring";
import { TopicProgress } from "./topic-progress";

type Filter<T extends string> = T | "all";

/** Practice (LeetCode's problem list): every lab, challenge and quiz of every course in one table, filtered by type,
 * difficulty, status and topic. */
export function Practice() {
  const [query, setQuery] = useState("");
  const [type, setType] = useState<Filter<Problem["type"]>>("all");
  const [level, setLevel] = useState<Filter<Problem["difficulty"]>>("all");
  const [status, setStatus] = useState<Filter<Problem["status"]>>("all");
  const [topic, setTopic] = useState<string | null>(null);
  const [runnable, setRunnable] = useState(true);
  const navigate = useNavigate();

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return problems.filter(
      (p) =>
        (type === "all" || p.type === type) &&
        (level === "all" || p.difficulty === level) &&
        (status === "all" || p.status === status) &&
        (!topic || p.topics.includes(topic)) &&
        (!runnable || !p.layout) &&
        (!q || `${p.title} ${p.course} ${p.topics.join(" ")}`.toLowerCase().includes(q)),
    );
  }, [query, type, level, status, topic, runnable]);

  function pick() {
    const open = shown.filter((p) => p.status !== "solved");
    const p = open[Math.floor(Math.random() * open.length)];
    if (p) void navigate({ to: "/learn/$course/$item", params: p.to });
  }

  return (
    <div className="grid gap-10 xl:grid-cols-[1fr_18rem]">
      <section aria-labelledby="practice" className="min-w-0">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 id="practice" className="font-serif text-4xl">
              Practice
            </h1>
            <p className="mt-2 text-ink-soft">
              Every lab, challenge and quiz of every course. Each runs on your lab, so a pass here counts in its course.
            </p>
          </div>
          <Button
            variant="outline"
            className="bg-paper"
            onClick={pick}
            disabled={!shown.some((p) => p.status !== "solved")}
          >
            <Shuffle aria-hidden />
            Pick one for me
          </Button>
        </div>

        <div className="mt-6 flex flex-wrap items-center gap-3">
          <div className="relative min-w-56 flex-1">
            <Search aria-hidden className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-ink-soft" />
            <Input
              type="search"
              aria-label="Search practice"
              placeholder="Search by title, course or topic"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
              }}
              className="bg-paper pl-9"
            />
          </div>
          <Choice
            label="Type"
            value={type}
            onChange={setType}
            options={{ lab: "Labs", challenge: "Challenges", quiz: "Quizzes" }}
          />
          <Choice
            label="Difficulty"
            value={level}
            onChange={setLevel}
            options={{ easy: "Easy", medium: "Medium", hard: "Hard" }}
          />
          <Choice
            label="Status"
            value={status}
            onChange={setStatus}
            options={{ solved: "Solved", attempted: "Attempted", todo: "Not started" }}
          />
          <span className="flex items-center gap-2 text-sm">
            <Switch id="runnable" checked={runnable} onCheckedChange={setRunnable} />
            <label htmlFor="runnable">Runs on this lab</label>
          </span>
        </div>

        <ul aria-label="Topics" className="mt-4 flex flex-wrap gap-2">
          {[null, ...topics].map((t) => (
            <li key={t ?? "all"}>
              <button
                type="button"
                aria-pressed={topic === t}
                onClick={() => {
                  setTopic(t);
                }}
                className={cn(
                  "rounded-full border-2 px-3 py-0.5 text-sm",
                  topic === t ? "border-ink bg-ink text-paper" : "border-ink/30 bg-paper hover:border-ink",
                )}
              >
                {t ?? "All topics"}
              </button>
            </li>
          ))}
        </ul>

        <div className="mt-6 overflow-hidden rounded-md border-2 border-ink">
          <table className="w-full text-left">
            <caption className="sr-only">
              {shown.length} of {problems.length} shown
            </caption>
            <thead className="border-b-2 border-ink bg-enamel text-sm">
              <tr>
                <th scope="col" className="w-12 px-3 py-2 font-semibold">
                  <span className="sr-only">Status</span>
                </th>
                <th scope="col" className="px-3 py-2 font-semibold">
                  Title
                </th>
                <th scope="col" className="hidden px-3 py-2 font-semibold md:table-cell">
                  Type
                </th>
                <th scope="col" className="px-3 py-2 font-semibold">
                  Difficulty
                </th>
                <th scope="col" className="hidden px-3 py-2 font-semibold sm:table-cell">
                  Time
                </th>
                <th scope="col" className="hidden px-3 py-2 text-right font-semibold lg:table-cell">
                  Your best
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-rule">
              {shown.map((p, i) => (
                <tr key={p.id} className={cn("relative hover:bg-enamel/60", i % 2 === 1 && "bg-enamel/25")}>
                  <td className="px-3 py-2.5">
                    <Status status={p.status} />
                  </td>
                  <td className="px-3 py-2.5">
                    <Link
                      to="/learn/$course/$item"
                      params={p.to}
                      className="font-semibold after:absolute after:inset-0 hover:underline"
                    >
                      {p.title}
                    </Link>
                    <span className="block text-sm text-ink-soft">
                      {p.course}, {p.section}
                    </span>
                  </td>
                  <td className="hidden px-3 py-2.5 md:table-cell">
                    <span className="flex items-center gap-2 text-sm">
                      <ItemIcon type={p.type} className="text-ink-soft" />
                      {typeName[p.type]}
                    </span>
                  </td>
                  <td className="px-3 py-2.5">
                    <DifficultyLabel level={p.difficulty} />
                  </td>
                  <td className="hidden px-3 py-2.5 text-sm text-ink-soft sm:table-cell">{duration(p.minutes)}</td>
                  <td className="hidden px-3 py-2.5 text-right text-sm lg:table-cell">{p.best ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {shown.length === 0 ? (
            <p className="px-4 py-8 text-center text-ink-soft">
              Nothing matches these filters. Clear the topic or the search to see more.
            </p>
          ) : null}
        </div>
        <p className="mt-2 text-sm text-ink-soft">
          {shown.length} of {problems.length} shown
        </p>
      </section>

      <aside aria-label="Your practice" className="space-y-6 xl:sticky xl:top-6 xl:self-start">
        <SolvedRing />
        <TopicProgress onPick={setTopic} />
      </aside>
    </div>
  );
}

function Status({ status }: { status: Problem["status"] }) {
  if (status === "solved") return <CircleCheck aria-label="Solved" className="size-5 text-green" />;
  if (status === "attempted") return <CircleDashed aria-label="Attempted" className="size-5 text-amber" />;
  return <span className="sr-only">Not started</span>;
}

function Choice<T extends string>({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: Filter<T>;
  onChange: (v: Filter<T>) => void;
  options: Record<T, string>;
}) {
  return (
    <Select
      value={value}
      onValueChange={(v) => {
        onChange(v as Filter<T>);
      }}
    >
      <SelectTrigger aria-label={label} className="w-36 bg-paper">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">{`Any ${label.toLowerCase()}`}</SelectItem>
        {(Object.entries(options) as [T, string][]).map(([v, name]) => (
          <SelectItem key={v} value={v}>
            {name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
