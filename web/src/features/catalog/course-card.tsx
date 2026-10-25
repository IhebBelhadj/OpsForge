import { Link } from "@tanstack/react-router";

import type { CourseSummary } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { count, duration, levelName } from "@/lib/format";

import { CourseArt } from "./course-art";

/** A course in the catalogue (Udemy's card): picture, title, subtitle, level, what is in it and how long. */
export function CourseCard({ course }: { course: CourseSummary }) {
  const c = course.item_counts;
  const parts = [
    count(course.section_count, "section"),
    c.lecture ? count(c.lecture, "lecture") : "",
    c.quiz ? count(c.quiz, "quiz", "quizzes") : "",
    c.lab ? count(c.lab, "lab") : "",
    c.challenge ? count(c.challenge, "challenge") : "",
  ].filter(Boolean);
  return (
    <li className="group relative flex flex-col overflow-hidden rounded-md border-2 border-ink bg-paper transition-shadow hover:shadow-[4px_4px_0_var(--ink)] focus-within:shadow-[4px_4px_0_var(--ink)]">
      <CourseArt sections={course.section_count} className="h-auto w-full border-b-2 border-ink" />
      <div className="flex flex-1 flex-col gap-2 p-4">
        <h3 className="text-lg leading-snug font-extrabold">
          {/* The whole card is the link's target (::after), the title its accessible name */}
          <Link
            to="/courses/$course"
            params={{ course: course.id }}
            className="after:absolute after:inset-0 focus-visible:outline-none"
          >
            {course.title}
          </Link>
        </h3>
        <p className="text-sm text-ink-soft">{course.subtitle}</p>
        <div className="mt-auto flex flex-wrap items-center gap-2 pt-2 text-sm">
          <Badge variant="outline" className="border-ink">
            {levelName[course.level]}
          </Badge>
          <span className="text-ink-soft">
            {parts.join(", ")} · {duration(course.minutes)}
          </span>
        </div>
      </div>
    </li>
  );
}
