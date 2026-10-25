import { Link } from "@tanstack/react-router";
import { Award, Check, CircleSlash, Flame, ListChecks, Medal, SquareTerminal } from "lucide-react";

import { DifficultyLabel } from "@/components/difficulty";
import { ItemIcon } from "@/components/item-icon";
import { Lamp } from "@/components/lamp";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { LineMap } from "@/features/roadmap";
import { typeName } from "@/lib/course";
import { duration, levelName } from "@/lib/format";
import { type Activity, activity, learner, levelFor, lines, practiceDays, problems } from "@/mock";

const resumeLine = lines.find((l) => l.title === learner.resume.course) ?? lines[0];

/** Home (Udemy's "My learning" with LeetCode's streak): pick up where you left off, keep the streak, see what is
 * next on your line and how the courses stand. */
export function Home() {
  const next = problems.filter((p) => p.course === learner.resume.course && p.status !== "solved").slice(0, 3);
  return (
    <div className="space-y-12">
      <div className="grid gap-6 lg:grid-cols-[1fr_20rem]">
        <Resume />
        <Streak />
      </div>

      <div className="grid gap-10 lg:grid-cols-[1fr_20rem]">
        <section aria-labelledby="next">
          <div className="flex items-baseline justify-between gap-4">
            <h2 id="next" className="text-xl font-extrabold">
              Next on your line
            </h2>
            <Link to="/practice" className="text-sm underline underline-offset-4">
              All practice
            </Link>
          </div>
          <ul className="mt-4 grid gap-4 sm:grid-cols-3">
            {next.map((p) => (
              <li
                key={p.id}
                className="relative flex flex-col gap-2 rounded-md border-2 border-ink bg-paper p-4 transition-shadow focus-within:shadow-[4px_4px_0_var(--ink)] hover:shadow-[4px_4px_0_var(--ink)]"
              >
                <span className="flex items-center gap-2 text-sm text-ink-soft">
                  <ItemIcon type={p.type} />
                  {typeName[p.type]}, {duration(p.minutes)}
                </span>
                <h3 className="leading-snug font-extrabold">
                  <Link to="/learn/$course/$item" params={p.to} className="after:absolute after:inset-0">
                    {p.title}
                  </Link>
                </h3>
                <span className="mt-auto flex items-center justify-between pt-2">
                  <DifficultyLabel level={p.difficulty} />
                  <span className="text-sm text-ink-soft">{p.xp} XP</span>
                </span>
              </li>
            ))}
          </ul>

          <h2 className="mt-12 text-xl font-extrabold">Your courses</h2>
          <ul className="mt-4 divide-y divide-rule rounded-md border-2 border-ink">
            {lines.map((l) => {
              const passed = l.stations.filter((s) => s.state === "passed").length;
              const pct = Math.round((passed / l.stations.length) * 100);
              const locked = l.stations.every((s) => s.state === "locked");
              return (
                <li
                  key={l.course}
                  className="grid items-center gap-x-6 gap-y-2 px-4 py-3 sm:grid-cols-[1fr_12rem_6rem]"
                >
                  <span>
                    <Link
                      to="/roadmap"
                      search={{ course: l.course }}
                      className="font-semibold underline-offset-4 hover:underline"
                    >
                      {l.title}
                    </Link>
                    <span className="block text-sm text-ink-soft">
                      {levelName[l.level]}
                      {locked ? `, after ${l.after.join(" and ")}` : ""}
                    </span>
                  </span>
                  <Progress value={pct} aria-label={`${l.title}: ${String(pct)} %`} />
                  <span className="text-sm text-ink-soft sm:text-right">
                    {pct === 100 ? "Completed" : locked ? "Not open yet" : `${String(pct)} %`}
                  </span>
                </li>
              );
            })}
          </ul>
        </section>

        <section aria-labelledby="recent">
          <h2 id="recent" className="text-xl font-extrabold">
            Recent
          </h2>
          <ol className="mt-4 space-y-4">
            {activity.map((a) => (
              <li key={a.at} className="flex gap-3">
                <ActivityIcon kind={a.kind} />
                <span className="min-w-0">
                  <span className="block leading-snug font-semibold">{a.title}</span>
                  <span className="block text-sm text-ink-soft">{a.detail}</span>
                  <time dateTime={a.at} className="block text-xs text-ink-soft">
                    {when(a.at)}
                  </time>
                </span>
              </li>
            ))}
          </ol>
        </section>
      </div>
    </div>
  );
}

function Resume() {
  const r = learner.resume;
  return (
    <section aria-labelledby="resume" className="overflow-hidden rounded-md border-2 border-ink bg-paper">
      <div className="px-5 pt-5 sm:px-6">
        <p className="text-ink-soft">Pick up where you left off</p>
        <h1 id="resume" className="mt-1 font-serif text-3xl leading-tight sm:text-4xl">
          {r.course}
        </h1>
        <p className="mt-2 flex items-center gap-2">
          <ItemIcon type={r.type} className="text-ink-soft" />
          {typeName[r.type]} {r.number}: {r.title}
        </p>
      </div>
      {resumeLine ? (
        <div className="mt-5 border-y-2 border-ink bg-enamel px-5 pt-6 pb-4 sm:px-6">
          <LineMap line={resumeLine} size="lg" />
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-x-6 gap-y-3 px-5 py-4 sm:px-6">
        <div className="min-w-40 flex-1">
          <Progress value={r.progress} aria-label={`Course progress: ${String(r.progress)} %`} />
          <p className="mt-1 text-sm text-ink-soft">{r.progress} % of the course</p>
        </div>
        <Button asChild size="lg">
          <Link to="/learn/$course/$item" params={{ course: r.courseId, item: r.item }}>
            Resume {typeName[r.type].toLowerCase()}
          </Link>
        </Button>
      </div>
    </section>
  );
}

const weekday = new Intl.DateTimeFormat("en", { weekday: "narrow" });

function Streak() {
  const recent = practiceDays().slice(-35);
  const weeks = [0, 1, 2, 3, 4].map((i) => recent.slice(i * 7, i * 7 + 7));
  const { level, floor, next } = levelFor(learner.xp);
  return (
    <section aria-labelledby="streak" className="flex flex-col rounded-md border-2 border-ink bg-enamel p-5">
      <h2 id="streak" className="flex items-center gap-2 font-extrabold">
        <Flame aria-hidden className="size-5 text-amber" />
        {learner.streak.current} days in a row
      </h2>
      <p className="mt-1 text-sm text-ink-soft">
        Best {learner.streak.best} days. Pass anything today to keep it going.
      </p>
      <div className="mt-4" aria-label="The last five weeks" role="group">
        <div aria-hidden className="grid grid-cols-7 gap-1 text-center text-xs text-ink-soft">
          {weeks[0]?.map((d) => (
            <span key={d.date}>{weekday.format(new Date(d.date))}</span>
          ))}
        </div>
        <ol className="mt-1.5 space-y-1.5">
          {weeks.map((w) => (
            <li key={w[0]?.date} className="grid grid-cols-7 gap-1 justify-items-center">
              {w.map((d) => (
                <span key={d.date} className="flex">
                  <Lamp on={d.minutes ? "green" : "off"} className="size-4" />
                  <span className="sr-only">
                    {d.date}: {d.minutes ? `${String(d.minutes)} minutes` : "no practice"}
                  </span>
                </span>
              ))}
            </li>
          ))}
        </ol>
        <p className="mt-3 text-sm text-ink-soft">
          {Math.round(recent.reduce((m, d) => m + d.minutes, 0) / 60)} h practised in five weeks
        </p>
      </div>
      <div className="mt-auto pt-6">
        <p className="flex items-baseline justify-between">
          <span className="font-extrabold">Level {level}</span>
          <span className="text-sm text-ink-soft">
            {learner.xp.toLocaleString("en")} / {next.toLocaleString("en")} XP
          </span>
        </p>
        <Progress
          value={((learner.xp - floor) / (next - floor)) * 100}
          aria-label={`Level ${String(level)} progress`}
          className="mt-1.5 bg-paper"
          indicatorClassName="bg-ink"
        />
        <p className="mt-1 text-sm text-ink-soft">+{learner.xpToday} XP today</p>
      </div>
    </section>
  );
}

const icons: Record<Activity["kind"], typeof Check> = {
  "lab.passed": SquareTerminal,
  "quiz.submitted": ListChecks,
  "challenge.accepted": Medal,
  "lecture.completed": Check,
  "badge.awarded": Award,
  "lab.abandoned": CircleSlash,
};

function ActivityIcon({ kind }: { kind: Activity["kind"] }) {
  const Icon = icons[kind];
  return (
    <span className="grid size-8 shrink-0 place-items-center rounded-full border-2 border-ink bg-enamel">
      <Icon aria-hidden className="size-4" />
    </span>
  );
}

const day = new Intl.DateTimeFormat("en", { weekday: "long", hour: "2-digit", minute: "2-digit" });

function when(at: string) {
  return day.format(new Date(at));
}
