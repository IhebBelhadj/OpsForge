import { Link } from "@tanstack/react-router";
import { Check, Circle } from "lucide-react";
import { useState } from "react";

import { ItemIcon } from "@/components/item-icon";
import { Lamp } from "@/components/lamp";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { type Line, lines, type Station } from "@/mock";
import { typeName } from "@/lib/course";
import { count, duration, levelName } from "@/lib/format";

import { LineMap } from "./line-map";

function find(id: string | undefined) {
  for (const line of lines)
    for (const [i, s] of line.stations.entries())
      if (s.id === id || line.course === id) return { line, station: s, index: i };
  return undefined;
}

/** Where the learner is: the current station, else the first one */
function here() {
  for (const line of lines)
    for (const [i, s] of line.stations.entries()) if (s.state === "current") return { line, station: s, index: i };
  return find(lines[0]?.course);
}

/** The roadmap (AlgoMonster's learning path, drawn as the signal box's mimic panel): every course a line leaving
 * one trunk, every section a station, lamps for where you are. Picking a station shows what it holds. */
export function Roadmap({ course }: { course?: string | undefined }) {
  const [picked, setPicked] = useState(() => find(course) ?? here());

  return (
    <div className="grid gap-10 xl:grid-cols-[1fr_22rem]">
      <section aria-labelledby="roadmap" className="min-w-0">
        <h1 id="roadmap" className="font-serif text-4xl">
          Roadmap
        </h1>
        <p className="mt-3 max-w-[60ch] text-lg text-ink-soft">
          Five courses on one network of lines, from your first route table to a cluster that loses a region. Pick a
          station to see what it holds.
        </p>
        <ul aria-label="What the lamps mean" className="mt-5 flex flex-wrap gap-x-5 gap-y-2 text-sm text-ink-soft">
          <li className="flex items-center gap-2">
            <Lamp on="green" /> Passed
          </li>
          <li className="flex items-center gap-2">
            <Lamp on="amber" /> You are here
          </li>
          <li className="flex items-center gap-2">
            <Lamp on="white" /> Open
          </li>
          <li className="flex items-center gap-2">
            <Lamp on="off" /> After an earlier course
          </li>
        </ul>

        {/* The trunk every line branches from */}
        <div className="relative mt-8 pl-8">
          <ol className="space-y-8">
            {lines.map((line, li) => {
              const passed = line.stations.filter((s) => s.state === "passed").length;
              const locked = line.stations.every((s) => s.state === "locked");
              return (
                <li key={line.course} className="relative">
                  {/* The trunk, down to the next line's junction, and this line's branch off it */}
                  {li < lines.length - 1 ? (
                    <span aria-hidden className="absolute top-3 -bottom-11 -left-6 w-1.5 bg-ink" />
                  ) : null}
                  <span aria-hidden className="absolute top-3 -left-6 h-1.5 w-5 rounded-l-full bg-ink" />
                  <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                    <h2 className="text-xl font-extrabold">{line.title}</h2>
                    <span className="text-sm text-ink-soft">
                      {levelName[line.level]}, {passed} of {count(line.stations.length, "station")} passed
                    </span>
                  </div>
                  {locked && line.after.length ? (
                    <p className="mt-1 text-sm text-ink-soft">After {line.after.join(" and ")}</p>
                  ) : null}
                  <div className="mt-3 rounded-md border-2 border-ink bg-enamel px-4 pt-5 pb-4 md:px-6">
                    <LineMap
                      grid
                      line={line}
                      selected={picked?.station.id}
                      onSelect={(station) => {
                        setPicked({ line, station, index: line.stations.indexOf(station) });
                        // Below xl the panel sits under the map: bring it into view
                        if (!window.matchMedia("(min-width: 80rem)").matches)
                          document.getElementById("station")?.scrollIntoView({ behavior: "smooth", block: "start" });
                      }}
                    />
                  </div>
                </li>
              );
            })}
          </ol>
        </div>
      </section>

      {picked ? <StationPanel line={picked.line} station={picked.station} index={picked.index} /> : null}
    </div>
  );
}

function StationPanel({ line, station, index }: { line: Line; station: Station; index: number }) {
  const done = station.items.filter((i) => i.done).length;
  const minutes = station.items.reduce((m, i) => m + i.minutes, 0);
  const next = station.items.find((i) => !i.done);
  return (
    <aside aria-labelledby="station" aria-live="polite" className="xl:sticky xl:top-6 xl:self-start">
      <div className="rounded-md border-2 border-ink bg-paper shadow-[4px_4px_0_var(--ink)]">
        <div className="border-b-2 border-ink bg-enamel px-5 py-4">
          <p className="text-sm text-ink-soft">
            {line.title}, station {index + 1} of {line.stations.length}
          </p>
          <h2 id="station" className="mt-1 font-serif text-2xl leading-tight">
            {station.title}
          </h2>
        </div>
        <div className="space-y-4 px-5 py-4">
          <ul aria-label="Topics" className="flex flex-wrap gap-1.5">
            {station.topics.map((t) => (
              <li key={t}>
                <Badge variant="outline" className="border-ink/50 font-normal">
                  {t}
                </Badge>
              </li>
            ))}
          </ul>
          <div>
            <p className="flex justify-between text-sm">
              <span>
                {done} of {count(station.items.length, "item")}
              </span>
              <span className="text-ink-soft">{duration(minutes)}</span>
            </p>
            <Progress value={(done / station.items.length) * 100} className="mt-1.5" aria-label="Station progress" />
          </div>
          <ol className="-mx-2">
            {station.items.map((item, i) => (
              <li key={item.title} className="flex items-center gap-3 rounded-md px-2 py-1.5">
                {item.done ? (
                  <Check aria-label="Done" className="size-4 shrink-0 text-green" strokeWidth={3} />
                ) : (
                  <Circle aria-label="Not done" className="size-4 shrink-0 text-lamp-off" />
                )}
                <ItemIcon type={item.type} className="text-ink-soft" />
                <span className="flex-1 text-sm">
                  {index + 1}.{i + 1} {item.title}
                  <span className="sr-only">, {typeName[item.type]}</span>
                </span>
                <span className="text-xs text-ink-soft">{duration(item.minutes)}</span>
              </li>
            ))}
          </ol>
          {station.state === "locked" ? (
            <p className="rounded-md border-2 border-dashed border-rule p-3 text-sm text-ink-soft">
              Opens after {line.after.join(" and ")}. You can read its lectures any time.
            </p>
          ) : next ? (
            <Button asChild className="w-full">
              <Link to="/learn/$course/$item" params={{ course: "k8s-intro", item: "what-is-a-pod" }}>
                {done ? "Continue" : "Start"}: {typeName[next.type]} {index + 1}.{station.items.indexOf(next) + 1}
              </Link>
            </Button>
          ) : (
            <Button asChild variant="outline" className="w-full bg-paper">
              <Link to="/learn/$course/$item" params={{ course: "k8s-intro", item: "what-is-a-pod" }}>
                Review this station
              </Link>
            </Button>
          )}
        </div>
      </div>
    </aside>
  );
}
