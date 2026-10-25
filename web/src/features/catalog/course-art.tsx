/** A course's picture (design.md §1): its sections as stations on a track, in the mimic panel's style. Generated,
 * so every course has one; `lit` stations (passed sections) glow green once progress exists. */
export function CourseArt({ sections, lit = 0, className }: { sections: number; lit?: number; className?: string }) {
  const n = Math.max(sections, 1);
  const w = 320;
  const h = 120;
  const pad = 36;
  const step = n > 1 ? (w - 2 * pad) / (n - 1) : 0;
  const xs = Array.from({ length: n }, (_, i) => (n > 1 ? pad + i * step : w / 2));
  return (
    <svg viewBox={`0 0 ${String(w)} ${String(h)}`} className={className} aria-hidden focusable="false">
      <rect width={w} height={h} className="fill-enamel" />
      <line x1={12} y1={h / 2} x2={w - 12} y2={h / 2} className="stroke-ink" strokeWidth={6} strokeLinecap="round" />
      {xs.map((x, i) => (
        <g key={x}>
          <circle
            cx={x}
            cy={h / 2}
            r={11}
            className={i < lit ? "fill-green stroke-ink" : "fill-lamp stroke-ink"}
            strokeWidth={3}
          />
          <text x={x} y={h / 2 + 34} textAnchor="middle" className="fill-ink-soft font-sans text-[13px] font-semibold">
            {i + 1}
          </text>
        </g>
      ))}
    </svg>
  );
}
