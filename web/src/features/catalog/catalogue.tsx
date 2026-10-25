import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { type CourseSummary, queries } from "@/api/client";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { levelName } from "@/lib/format";

import { CourseCard } from "./course-card";

type Level = CourseSummary["level"] | "all";

/** The catalogue (design.md §1): every live course, filtered by level. "Continue learning" comes with progress. */
export function Catalogue() {
  const courses = useQuery(queries.courses());
  const [level, setLevel] = useState<Level>("all");
  const shown = (courses.data ?? []).filter((c) => level === "all" || c.level === level);

  return (
    <section aria-labelledby="catalogue">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <h1 id="catalogue" className="font-serif text-4xl">
          Courses
        </h1>
        {courses.data?.length ? (
          <div className="flex items-center gap-2 text-sm">
            <span id="level-label" className="text-ink-soft">
              Level
            </span>
            <Select
              value={level}
              onValueChange={(v) => {
                setLevel(v as Level);
              }}
            >
              <SelectTrigger aria-labelledby="level-label" className="w-40 bg-paper">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All levels</SelectItem>
                {Object.entries(levelName).map(([value, name]) => (
                  <SelectItem key={value} value={value}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ) : null}
      </div>

      {courses.isPending ? (
        <ul aria-label="Loading courses" className="mt-8 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <li key={i}>
              <Skeleton className="h-64 rounded-md" />
            </li>
          ))}
        </ul>
      ) : courses.isError ? (
        <p role="alert" className="mt-6 text-red">
          The courses could not be loaded: {courses.error.message}
        </p>
      ) : courses.data.length === 0 ? (
        <p className="mt-4 max-w-prose text-lg text-ink-soft">
          No courses yet. Courses come from the packs in <code className="font-mono text-base">labs/</code>, once the
          platform imports them.
        </p>
      ) : (
        <>
          <h2 className="sr-only">All courses</h2>
          {shown.length ? (
            <ul className="mt-8 grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
              {shown.map((c) => (
                <CourseCard key={c.id} course={c} />
              ))}
            </ul>
          ) : (
            <p className="mt-6 text-ink-soft">No course at this level yet.</p>
          )}
        </>
      )}
    </section>
  );
}
