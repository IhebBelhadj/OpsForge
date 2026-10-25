import { useEffect, useRef } from "react";

import { cn } from "@/lib/utils";

const month = new Intl.DateTimeFormat("en", { month: "short" });
const long = new Intl.DateTimeFormat("en", { dateStyle: "medium" });

function shade(minutes: number) {
  if (!minutes) return "bg-enamel-deep";
  if (minutes < 25) return "bg-green/35";
  if (minutes < 50) return "bg-green/65";
  return "bg-green";
}

/** LeetCode's year of submissions: one cell per day, weeks as columns, green by minutes practised. */
export function Heatmap({ days }: { days: { date: string; minutes: number }[] }) {
  // Pad the start so the first column begins on a Monday
  const first = new Date(days[0]?.date ?? 0);
  const pad = (first.getDay() + 6) % 7;
  const cells: ({ date: string; minutes: number } | null)[] = [...Array<null>(pad).fill(null), ...days];
  const weeks: (typeof cells)[] = [];
  for (let i = 0; i < cells.length; i += 7) weeks.push(cells.slice(i, i + 7));
  const active = days.filter((d) => d.minutes).length;
  // On a narrow screen the year scrolls: start at the recent end, as LeetCode does
  const scroller = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = scroller.current;
    if (el) el.scrollLeft = el.scrollWidth;
  }, []);

  return (
    <figure>
      <div ref={scroller} className="overflow-x-auto pb-2">
        <div
          className="inline-grid auto-cols-[13px] grid-flow-col gap-[3px]"
          role="img"
          aria-label={`${String(active)} days of practice in the last year`}
        >
          {weeks.map((w, i) => {
            const start = w.find(Boolean);
            const label = start && new Date(start.date).getDate() <= 7 ? month.format(new Date(start.date)) : "";
            return (
              <div key={start?.date ?? i} className="grid grid-rows-[16px_repeat(7,13px)] gap-[3px]">
                <span className="text-[10px] leading-none whitespace-nowrap text-ink-soft">{label}</span>
                {w.map((d, j) =>
                  d ? (
                    <span
                      key={d.date}
                      title={`${long.format(new Date(d.date))}: ${d.minutes ? `${String(d.minutes)} min` : "no practice"}`}
                      className={cn("rounded-[2px]", shade(d.minutes))}
                    />
                  ) : (
                    <span key={`pad${String(j)}`} />
                  ),
                )}
              </div>
            );
          })}
        </div>
      </div>
      <figcaption className="mt-2 flex flex-wrap items-center justify-between gap-2 text-sm text-ink-soft">
        <span>{active} days of practice in the last year</span>
        <span className="flex items-center gap-1.5" aria-hidden>
          Less
          {[0, 10, 30, 60].map((m) => (
            <span key={m} className={cn("size-[13px] rounded-[2px]", shade(m))} />
          ))}
          More
        </span>
      </figcaption>
    </figure>
  );
}
