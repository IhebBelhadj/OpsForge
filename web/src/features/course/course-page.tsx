import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { useState } from "react";

import { type Course, isNotFound, queries } from "@/api/client";
import { ItemIcon } from "@/components/item-icon";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { CourseArt } from "@/features/catalog";
import { Markdown } from "@/features/lecture";
import { placed, typeName } from "@/lib/course";
import { count, duration, levelName } from "@/lib/format";

/** The course page (design.md §2, Udemy's course landing). */
export function CoursePage({ courseId }: { courseId: string }) {
  const query = useQuery(queries.course(courseId));
  if (query.isPending) return <Skeleton aria-label="Loading the course" className="h-96 rounded-lg" />;
  if (query.isError)
    return isNotFound(query.error) ? (
      <section>
        <h1 className="text-2xl font-semibold">No such course</h1>
        <p className="mt-3 text-ink-soft">
          It is not in the catalogue, or was removed from the packs.{" "}
          <Link to="/courses" className="underline underline-offset-4">
            Back to the courses
          </Link>
        </p>
      </section>
    ) : (
      <p role="alert" className="text-red">
        The course could not be loaded: {query.error.message}
      </p>
    );
  return <Loaded course={query.data} />;
}

function Loaded({ course }: { course: Course }) {
  const items = placed(course);
  const first = items[0];
  const c = course.item_counts;
  const contents = [
    count(course.section_count, "section"),
    c.lecture ? count(c.lecture, "lecture") : "",
    c.quiz ? count(c.quiz, "quiz", "quizzes") : "",
    c.lab ? count(c.lab, "lab") : "",
    c.challenge ? count(c.challenge, "challenge") : "",
  ].filter(Boolean);
  const [open, setOpen] = useState<string[]>(course.sections.length ? ["s0"] : []);
  const allOpen = open.length === course.sections.length;

  return (
    <div>
      {/* Udemy's title band, full bleed: the margins reach the window's edges and the padding brings the content back
          onto the column (html clips the scrollbar's few pixels of overhang) */}
      <div className="mx-[calc(50%-50vw)] -mt-8 bg-band px-[calc(50vw-50%)] pt-6 pb-8 text-band-ink md:-mt-10">
        <Breadcrumb>
          <BreadcrumbList className="text-band-soft">
            <BreadcrumbItem>
              <BreadcrumbLink asChild className="hover:text-band-ink">
                <Link to="/courses">Courses</Link>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage className="text-band-soft">{course.title}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>

        <div className="mt-4 grid gap-8 lg:grid-cols-[1fr_20rem]">
          <div className="min-w-0">
            <h1 className="text-[1.75rem] leading-tight font-semibold tracking-[-0.01em]">{course.title}</h1>
            <p className="mt-3 max-w-[65ch] text-lg text-band-soft">{course.subtitle}</p>
            <p className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
              <span className="rounded-sm bg-ember-soft px-2 py-0.5 font-medium text-ember-ink">
                {levelName[course.level]}
              </span>
              <span className="text-band-soft">
                {contents.join(", ")} · {duration(course.minutes)}
              </span>
            </p>
          </div>

          <aside aria-label="Start" className="relative z-10 text-ink lg:-mb-64 lg:self-start">
            <div className="overflow-hidden rounded-lg border border-rule bg-surface">
              <CourseArt sections={course.section_count} className="h-auto w-full" />
              <div className="space-y-3 p-4">
                {first ? (
                  <Button asChild size="lg" className="w-full">
                    <Link to="/learn/$course/$item" params={{ course: course.id, item: first.item.id }}>
                      Start course
                    </Link>
                  </Button>
                ) : null}
                {course.needs.length || course.layout ? (
                  <p className="text-sm text-ink-soft">
                    Needs: {[...course.needs, ...(course.layout ? [`${course.layout} layout`] : [])].join(", ")}
                  </p>
                ) : null}
                {course.requires.length ? (
                  <p className="text-sm text-ink-soft">After: {course.requires.join(", ")}</p>
                ) : null}
              </div>
            </div>
          </aside>
        </div>
      </div>

      {course.outcomes.length ? (
        <section aria-labelledby="outcomes" className="mt-8 max-w-3xl rounded-lg border border-rule bg-surface p-5">
          <h2 id="outcomes" className="text-lg font-semibold">
            What you'll learn
          </h2>
          <ul className="mt-3 grid gap-x-6 gap-y-2 sm:grid-cols-2">
            {course.outcomes.map((o) => (
              <li key={o} className="flex gap-2 text-[0.9375rem]">
                <Check aria-hidden className="mt-1 size-4 shrink-0 text-ink-soft" />
                {o}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section aria-labelledby="content" className="mt-10 max-w-3xl">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id="content" className="text-lg font-semibold">
            Course content
          </h2>
          <Button
            variant="link"
            className="px-0"
            onClick={() => {
              setOpen(allOpen ? [] : course.sections.map((_, i) => `s${String(i)}`));
            }}
          >
            {allOpen ? "Collapse all" : "Expand all"}
          </Button>
        </div>
        <p className="text-sm text-ink-soft">
          {count(course.section_count, "section")} · {count(course.items, "item")} · {duration(course.minutes)}
        </p>
        <Accordion
          type="multiple"
          value={open}
          onValueChange={setOpen}
          className="mt-3 overflow-hidden rounded-lg border border-rule bg-surface"
        >
          {course.sections.map((s, si) => (
            <AccordionItem key={s.title} value={`s${String(si)}`} className="border-rule last:border-b-0">
              <AccordionTrigger className="rounded-none bg-enamel px-4 hover:no-underline">
                <span className="flex flex-1 flex-wrap items-baseline justify-between gap-x-4">
                  <span className="font-semibold">
                    {si + 1} &nbsp;{s.title}
                  </span>
                  <span className="text-sm font-normal text-ink-soft">
                    {count(s.items.length, "item")} · {duration(s.minutes)}
                  </span>
                </span>
              </AccordionTrigger>
              <AccordionContent className="pb-0">
                <ol>
                  {items
                    .filter((p) => p.section === si)
                    .map(({ item, number }) => (
                      <li key={item.id}>
                        <Link
                          to="/learn/$course/$item"
                          params={{ course: course.id, item: item.id }}
                          className="flex items-center gap-3 px-4 py-2.5 text-[0.9375rem] hover:bg-paper"
                        >
                          <ItemIcon type={item.type} className="text-ink-soft" />
                          <span className="w-20 shrink-0 text-sm text-ink-soft">{typeName[item.type]}</span>
                          <span className="flex-1">
                            {number} {item.title}
                          </span>
                          <span className="text-sm text-ink-soft">{duration(item.minutes)}</span>
                        </Link>
                      </li>
                    ))}
                </ol>
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </section>

      {course.requirements.length ? (
        <section aria-labelledby="requirements" className="mt-10 max-w-3xl">
          <h2 id="requirements" className="text-lg font-semibold">
            Requirements
          </h2>
          <ul className="mt-2 list-disc pl-6">
            {course.requirements.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        </section>
      ) : null}

      <section aria-labelledby="description" className="mt-10 max-w-3xl">
        <h2 id="description" className="text-lg font-semibold">
          Description
        </h2>
        <Markdown className="mt-2">{course.description}</Markdown>
      </section>
    </div>
  );
}
