import { DifficultyLabel } from "@/components/difficulty";
import { solvedByDifficulty } from "@/mock";

const order = ["easy", "medium", "hard"] as const;
const stroke = { easy: "stroke-green", medium: "stroke-amber", hard: "stroke-red" } as const;

/** LeetCode's solved ring: one arc per difficulty, the total in the middle, the counts beside it. */
export function SolvedRing() {
  const s = solvedByDifficulty();
  const done = order.reduce((n, d) => n + s[d].done, 0);
  const total = order.reduce((n, d) => n + s[d].total, 0);
  const r = 52;
  const c = 2 * Math.PI * r;
  return (
    <section aria-labelledby="solved" className="self-start rounded-lg border border-rule bg-surface p-5">
      <h2 id="solved" className="font-semibold">
        Solved
      </h2>
      <div className="mt-3 flex items-center gap-5">
        <svg viewBox="0 0 128 128" className="size-32 shrink-0 -rotate-90" aria-hidden>
          <circle cx={64} cy={64} r={r} className="fill-none stroke-paper" strokeWidth={10} />
          {order.map((d, i) => {
            const len = (s[d].done / total) * c;
            const at = order.slice(0, i).reduce((sum, e) => sum + (s[e].done / total) * c, 0);
            const arc = (
              <circle
                key={d}
                cx={64}
                cy={64}
                r={r}
                className={`fill-none ${stroke[d]}`}
                strokeWidth={10}
                strokeDasharray={`${String(Math.max(len - 3, 0))} ${String(c)}`}
                strokeDashoffset={-at}
              />
            );
            return arc;
          })}
          <text
            x={64}
            y={64}
            className="fill-ink font-sans text-[28px] font-semibold"
            textAnchor="middle"
            dominantBaseline="central"
            transform="rotate(90 64 64)"
          >
            {done}
          </text>
        </svg>
        <dl className="flex-1 space-y-2">
          {order.map((d) => (
            <div key={d} className="flex items-center justify-between gap-2">
              <dt>
                <DifficultyLabel level={d} />
              </dt>
              <dd className="text-sm">
                <span className="font-semibold">{s[d].done}</span>
                <span className="text-ink-soft">/{s[d].total}</span>
              </dd>
            </div>
          ))}
        </dl>
      </div>
      <p className="mt-3 text-sm text-ink-soft">
        {done} of {total} labs and challenges passed
      </p>
    </section>
  );
}
