import { Link } from "@tanstack/react-router";

import type { CourseSummary } from "@/api/client";
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
    <li className="group relative flex flex-col">
      <div className="overflow-hidden rounded-lg">
        <CourseArt
          sections={course.section_count}
          className="h-auto w-full transition-transform duration-300 group-hover:scale-[1.03] motion-reduce:transition-none"
        />
      </div>
      <div className="flex flex-1 flex-col gap-1 pt-3">
        <h3 className="leading-snug font-semibold">
          {/* The whole card is the link's target (::after), the title its accessible name */}
          <Link
            to="/courses/$course"
            params={{ course: course.id }}
            className="after:absolute after:inset-0 group-hover:underline focus-visible:outline-none"
          >
            {course.title}
          </Link>
        </h3>
        <p className="line-clamp-2 text-sm text-ink-soft">{course.subtitle}</p>
        <p className="mt-1 text-xs text-ink-soft">
          {parts.join(", ")} · {duration(course.minutes)}
        </p>
        <span className="mt-1.5 w-fit rounded-sm bg-enamel px-2 py-0.5 text-xs font-medium text-ink-soft">
          {levelName[course.level]}
        </span>
      </div>
    </li>
  );
}
