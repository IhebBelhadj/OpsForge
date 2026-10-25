import { Award } from "lucide-react";

import { Progress } from "@/components/ui/progress";
import { SolvedRing } from "@/features/practice";
import { cn } from "@/lib/utils";
import { badges, learner, levelFor, lines, practiceDays } from "@/mock";

import { Heatmap } from "./heatmap";

const date = new Intl.DateTimeFormat("en", { dateStyle: "medium" });

/** The profile (LeetCode's): level and XP, the streak, a year of practice, what is solved, badges, courses. */
export function Profile() {
  const { level, floor, next } = levelFor(learner.xp);
  const earned = badges.filter((b) => b.earned);
  const stats = [
    ["Total XP", learner.xp.toLocaleString("en")],
    ["Current streak", `${String(learner.streak.current)} days`],
    ["Best streak", `${String(learner.streak.best)} days`],
    ["Badges", `${String(earned.length)} of ${String(badges.length)}`],
  ];
  return (
    <div className="space-y-10">
      <header className="flex flex-wrap items-center gap-6">
        <span className="grid size-20 place-items-center rounded-full border-4 border-ink bg-enamel font-serif text-4xl">
          {learner.name.slice(0, 1).toUpperCase()}
        </span>
        <div className="min-w-60 flex-1">
          <h1 className="font-serif text-4xl">Your profile</h1>
          <p className="mt-1 text-ink-soft">
            Signed in on this machine as <span className="font-semibold text-ink">{learner.name}</span>. Nothing leaves
            127.0.0.1.
          </p>
        </div>
        <div className="w-full max-w-xs">
          <p className="flex items-baseline justify-between">
            <span className="text-xl font-extrabold">Level {level}</span>
            <span className="text-sm text-ink-soft">
              {(next - learner.xp).toLocaleString("en")} XP to level {level + 1}
            </span>
          </p>
          <Progress
            value={((learner.xp - floor) / (next - floor)) * 100}
            aria-label={`Level ${String(level)} progress`}
            className="mt-1.5"
            indicatorClassName="bg-ink"
          />
        </div>
      </header>

      <dl className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {stats.map(([term, value]) => (
          <div key={term} className="rounded-md border-2 border-ink p-4">
            <dt className="text-sm text-ink-soft">{term}</dt>
            <dd className="mt-1 text-2xl font-extrabold">{value}</dd>
          </div>
        ))}
      </dl>

      <section aria-labelledby="year" className="rounded-md border-2 border-ink bg-paper p-5">
        <h2 id="year" className="mb-4 text-xl font-extrabold">
          A year of practice
        </h2>
        <Heatmap days={practiceDays()} />
      </section>

      <div className="grid gap-10 lg:grid-cols-[18rem_1fr]">
        <SolvedRing />
        <section aria-labelledby="badges">
          <h2 id="badges" className="text-xl font-extrabold">
            Badges
          </h2>
          <ul className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {badges.map((b) => (
              <li
                key={b.id}
                className={cn(
                  "flex gap-3 rounded-md border-2 p-3",
                  b.earned ? "border-ink bg-paper" : "border-dashed border-rule text-ink-soft",
                )}
              >
                <span
                  className={cn(
                    "grid size-10 shrink-0 place-items-center rounded-full border-2",
                    b.earned ? "border-ink bg-green text-paper" : "border-rule bg-enamel-deep",
                  )}
                >
                  <Award aria-hidden className="size-5" />
                </span>
                <span>
                  <span className={cn("block leading-snug", b.earned && "font-extrabold")}>{b.title}</span>
                  <span className="block text-sm text-ink-soft">{b.rule}</span>
                  <span className="block text-xs text-ink-soft">
                    {b.earned ? `Earned ${date.format(new Date(b.earned))}` : "Not earned yet"}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </section>
      </div>

      <section aria-labelledby="courses">
        <h2 id="courses" className="text-xl font-extrabold">
          Courses
        </h2>
        <ul className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {lines.map((l) => {
            const passed = l.stations.filter((s) => s.state === "passed").length;
            return (
              <li key={l.course} className="rounded-md border-2 border-ink p-4">
                <p className="font-semibold">{l.title}</p>
                <p className="mt-1 text-sm text-ink-soft">
                  {passed === l.stations.length
                    ? "Completed"
                    : `${String(passed)} of ${String(l.stations.length)} sections`}
                </p>
                <Progress
                  value={(passed / l.stations.length) * 100}
                  className="mt-3"
                  aria-label={`${l.title} progress`}
                />
              </li>
            );
          })}
        </ul>
      </section>
    </div>
  );
}
