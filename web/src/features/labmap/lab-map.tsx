import { links, sites } from "@/mock";
import { cn } from "@/lib/utils";

const linkClass = { nominal: "stroke-ink", changed: "stroke-amber", cut: "stroke-red" } as const;
const linkName = { nominal: "as configured", changed: "changed live", cut: "cut" } as const;

/** The mimic panel (design.md): the lab's sites and the WAN links between them, lamps for each link's state, as the
 * Phase 10 signal box drew them. Live data comes with `labstate`. */
export function LabMap({ className }: { className?: string }) {
  const at = Object.fromEntries(sites.map((s) => [s.id, s]));
  return (
    <figure className={cn("flex flex-col", className)}>
      <svg viewBox="0 0 600 330" className="h-auto w-full" role="img" aria-labelledby="labmap-title">
        <title id="labmap-title">
          {`Lab map: ${links.map((l) => `link ${l.from} to ${l.to} ${linkName[l.state]}, ${l.rtt}`).join("; ")}`}
        </title>
        <rect width={600} height={330} rx={6} className="fill-enamel" />
        {links.map((l) => {
          const a = at[l.from];
          const b = at[l.to];
          if (!a || !b) return null;
          const mx = (a.x + b.x) / 2;
          const my = (a.y + b.y) / 2;
          return (
            <g key={`${l.from}${l.to}`}>
              <line
                x1={a.x}
                y1={a.y}
                x2={b.x}
                y2={b.y}
                className={linkClass[l.state]}
                strokeWidth={7}
                strokeLinecap="round"
                strokeDasharray={l.state === "cut" ? "4 14" : undefined}
              />
              <rect
                x={mx - 34}
                y={my - 13}
                width={68}
                height={26}
                rx={13}
                className="fill-paper stroke-ink"
                strokeWidth={2}
              />
              <text x={mx} y={my + 5} textAnchor="middle" className="fill-ink font-sans text-[13px] font-semibold">
                {l.rtt}
              </text>
            </g>
          );
        })}
        {sites.map((s) => (
          <g key={s.id}>
            <rect
              x={s.x - 70}
              y={s.y - 34}
              width={140}
              height={68}
              rx={6}
              className="fill-paper stroke-ink"
              strokeWidth={3}
            />
            <text x={s.x - 56} y={s.y - 10} className="fill-ink font-sans text-[14px] font-semibold">
              {s.name}
            </text>
            {s.nodes.map((n, i) => (
              <g key={n}>
                <circle
                  cx={s.x - 50 + i * 62}
                  cy={s.y + 14}
                  r={6}
                  className="fill-green stroke-ink"
                  strokeWidth={1.5}
                />
                <text x={s.x - 40 + i * 62} y={s.y + 19} className="fill-ink-soft font-mono text-[12px]">
                  {n}
                </text>
              </g>
            ))}
          </g>
        ))}
      </svg>
      <figcaption className="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-sm text-ink-soft">
        <span className="flex items-center gap-2">
          <span aria-hidden className="h-1.5 w-6 rounded-full bg-ink" /> As configured
        </span>
        <span className="flex items-center gap-2">
          <span aria-hidden className="h-1.5 w-6 rounded-full bg-amber" /> Changed live
        </span>
        <span className="flex items-center gap-2">
          <span aria-hidden className="h-1.5 w-6 rounded-full border-t-4 border-dotted border-red" /> Cut
        </span>
        <span className="flex items-center gap-2">
          <span aria-hidden className="size-2.5 rounded-full bg-green" /> Node ready
        </span>
      </figcaption>
    </figure>
  );
}
