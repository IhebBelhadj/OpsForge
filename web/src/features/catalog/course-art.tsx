/** A course's picture (design.md §1): its sections as stations on a track, drawn on the dark band like a thumbnail.
 * Generated, so every course has one; `lit` stations (passed sections) turn green once progress exists, and the
 * next one is ember. */
export function CourseArt({ sections, lit = 0, className }: { sections: number; lit?: number; className?: string }) {
  const n = Math.max(sections, 1);
  const w = 320;
  const h = 180;
  const pad = 48;
  const step = n > 1 ? (w - 2 * pad) / (n - 1) : 0;
  const xs = Array.from({ length: n }, (_, i) => (n > 1 ? pad + i * step : w / 2));
  const y = h / 2;
  return (
    <svg viewBox={`0 0 ${String(w)} ${String(h)}`} className={className} aria-hidden focusable="false">
      <rect width={w} height={h} className="fill-band" />
      <line x1={0} y1={y} x2={w} y2={y} className="stroke-band-soft/30" strokeWidth={3} />
      {xs.map((x, i) => (
        <circle
          key={x}
          cx={x}
          cy={y}
          r={i === lit ? 11 : 8}
          className={i < lit ? "fill-green" : i === lit ? "fill-ember" : "fill-band stroke-band-soft"}
          strokeWidth={2.5}
        />
      ))}
    </svg>
  );
}
