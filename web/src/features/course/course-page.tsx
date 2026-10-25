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
  if (query.isPending) return <Skeleton aria-label="Loading the course" className="h-96 rounded-md" />;
  if (query.isError)
    return isNotFound(query.error) ? (
      <section>
        <h1 className="font-serif text-3xl">No such course</h1>
        <p className="mt-3 text-ink-soft">
          It is not in the catalogue, or was removed from the packs.{" "}
          <Link to="/" className="underline underline-offset-4">
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
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link to="/">Courses</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>{course.title}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>

      <div className="mt-4 grid gap-8 lg:grid-cols-[1fr_20rem]">
        <div className="min-w-0">
          <h1 className="font-serif text-4xl leading-tight">{course.title}</h1>
          <p className="mt-3 text-lg text-ink-soft">{course.subtitle}</p>
          <p className="mt-3 text-sm">
            {levelName[course.level]} · {contents.join(", ")} · {duration(course.minutes)}
          </p>

          {course.outcomes.length ? (
            <section aria-labelledby="outcomes" className="mt-8 rounded-md border-2 border-ink p-5">
              <h2 id="outcomes" className="text-xl font-extrabold">
                What you'll learn
              </h2>
              <ul className="mt-3 grid gap-x-6 gap-y-2 sm:grid-cols-2">
                {course.outcomes.map((o) => (
                  <li key={o} className="flex gap-2">
                    <Check aria-hidden className="mt-1 size-4 shrink-0 text-green" />
                    {o}
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
        </div>

        <aside aria-label="Start" className="lg:sticky lg:top-6 lg:self-start">
          <div className="overflow-hidden rounded-md border-2 border-ink bg-paper">
            <CourseArt sections={course.section_count} className="h-auto w-full border-b-2 border-ink" />
            <div className="space-y-3 p-4">
              {first ? (
                <Button asChild className="w-full">
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

      <section aria-labelledby="content" className="mt-10 max-w-3xl">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <h2 id="content" className="text-xl font-extrabold">
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
        <Accordion type="multiple" value={open} onValueChange={setOpen} className="mt-3 rounded-md border-2 border-ink">
          {course.sections.map((s, si) => (
            <AccordionItem key={s.title} value={`s${String(si)}`} className="border-ink/30 last:border-b-0">
              <AccordionTrigger className="bg-enamel px-4 hover:no-underline">
                <span className="flex flex-1 flex-wrap items-baseline justify-between gap-x-4">
                  <span className="font-extrabold">
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
                          className="flex items-center gap-3 px-4 py-2.5 hover:bg-enamel/50"
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
          <h2 id="requirements" className="text-xl font-extrabold">
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
        <h2 id="description" className="text-xl font-extrabold">
          Description
        </h2>
        <Markdown className="mt-2">{course.description}</Markdown>
      </section>
    </div>
  );
}
