import { Check, Lock } from "lucide-react";

import type { Line, Station, StationState } from "@/mock";
import { cn } from "@/lib/utils";

const stateName: Record<StationState, string> = {
  passed: "passed",
  current: "you are here",
  started: "started",
  open: "open",
  locked: "locked",
};

const lampClass: Record<StationState, string> = {
  passed: "border-green bg-green",
  current: "border-ember bg-ember station-current",
  started: "border-green bg-surface",
  open: "border-control bg-surface",
  locked: "border-rule bg-enamel",
};

/** Whether the track from this station to the next is laid (solid) or still planned (dashed). */
function laid(from: Station, to: Station | undefined) {
  return to !== undefined && from.state !== "locked" && to.state !== "locked";
}

/** One course as a line of stations (sections), like a learning path: a dot per state, track between them.
 * Horizontal from md up, vertical on phones. Stations are buttons when `onSelect` is given. */
export function LineMap({
  line,
  selected,
  onSelect,
  size = "md",
  grid = false,
}: {
  line: Line;
  selected?: string | undefined;
  onSelect?: (station: Station) => void;
  size?: "md" | "lg";
  /** Fixed station spacing (eight to a line), so stations of different lines align as on a transit map */
  grid?: boolean;
}) {
  const dot = size === "lg" ? "size-7 border-2" : "size-6 border-2";
  const offset = size === "lg" ? "md:top-[13px] left-[13px]" : "md:top-[11px] left-[11px]";
  return (
    <ol className="flex flex-col md:flex-row">
      {line.stations.map((s, i) => {
        const next = line.stations[i + 1];
        const label = `${String(i + 1)}. ${s.title}, ${stateName[s.state]}`;
        const lamp = (
          <span className={cn("relative z-10 grid shrink-0 place-items-center rounded-full", dot, lampClass[s.state])}>
            {s.state === "passed" ? <Check aria-hidden className="size-3.5 text-white" strokeWidth={3} /> : null}
            {s.state === "locked" ? <Lock aria-hidden className="size-3 text-ink-soft" strokeWidth={2.5} /> : null}
          </span>
        );
        const text = (
          <span className="min-w-0 md:mt-2 md:px-1">
            <span className="block text-xs text-ink-soft">Section {i + 1}</span>
            <span
              className={cn(
                "block text-sm leading-snug",
                s.state === "locked" ? "text-ink-soft" : "font-medium",
                selected === s.id && "text-ember-ink underline decoration-2 underline-offset-4",
              )}
            >
              {s.title}
            </span>
          </span>
        );
        return (
          <li key={s.id} className={cn("relative pb-5 md:pb-0", grid ? "md:flex-none md:basis-[12.5%]" : "flex-1")}>
            {next ? (
              <span
                aria-hidden
                className={cn(
                  "absolute top-[10px] bottom-[-10px] w-0.5 md:right-[-50%] md:bottom-auto md:left-1/2 md:h-0.5 md:w-auto",
                  offset,
                  laid(s, next)
                    ? s.state === "passed"
                      ? "bg-green"
                      : "bg-control"
                    : "border-control bg-transparent max-md:border-l-2 max-md:border-dashed md:border-t-2 md:border-dashed",
                )}
              />
            ) : null}
            {onSelect ? (
              <button
                type="button"
                aria-label={label}
                aria-pressed={selected === s.id}
                onClick={() => {
                  onSelect(s);
                }}
                className="group flex w-full items-start gap-3 rounded-md text-left md:flex-col md:items-center md:text-center"
              >
                {lamp}
                {text}
              </button>
            ) : (
              <div className="flex items-start gap-3 md:flex-col md:items-center md:text-center">
                <span className="sr-only">{label}</span>
                {lamp}
                <span aria-hidden>{text}</span>
              </div>
            )}
          </li>
        );
      })}
    </ol>
  );
}
