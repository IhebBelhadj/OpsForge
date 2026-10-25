import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, ListTree } from "lucide-react";
import { useEffect } from "react";

import { type Course, isNotFound, queries } from "@/api/client";
import { ThemeToggle } from "@/app/theme-toggle";
import { ItemIcon } from "@/components/item-icon";
import { Button } from "@/components/ui/button";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  useSidebar,
} from "@/components/ui/sidebar";
import { Skeleton } from "@/components/ui/skeleton";
import { LectureView } from "@/features/lecture";
import { placed, type Placed, typeName } from "@/lib/course";
import { duration } from "@/lib/format";

import { ChallengePreview, LabPreview, QuizPreview } from "./previews";

/** The course player (design.md §3): top bar with the course and Prev/Next, the item, the curriculum on the right. */
export function Player({ courseId, itemId }: { courseId: string; itemId: string }) {
  const course = useQuery(queries.course(courseId));
  if (course.isPending) return <Skeleton aria-label="Loading the course" className="m-6 h-96 rounded-md" />;
  if (course.isError) return <Missing what="course" error={course.error} />;
  return (
    <SidebarProvider style={{ "--sidebar-width": "20rem" } as React.CSSProperties}>
      <Loaded course={course.data} itemId={itemId} />
    </SidebarProvider>
  );
}

function Missing({ what, error }: { what: string; error: Error | "not-found" }) {
  return (
    <section className="mx-auto max-w-[70ch] p-6">
      {error === "not-found" || isNotFound(error) ? (
        <>
          <h1 className="font-serif text-3xl">No such {what}</h1>
          <p className="mt-3 text-ink-soft">
            It is not in the catalogue, or was removed from the packs.{" "}
            <Link to="/" className="underline underline-offset-4">
              Back to the courses
            </Link>
          </p>
        </>
      ) : (
        <p role="alert" className="text-red">
          The {what} could not be loaded: {error.message}
        </p>
      )}
    </section>
  );
}

function editing(target: EventTarget | null) {
  return (
    target instanceof HTMLElement && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))
  );
}

function Loaded({ course, itemId }: { course: Course; itemId: string }) {
  const items = placed(course);
  const at = items.findIndex((p) => p.item.id === itemId);
  const current = items[at];
  const prev = at > 0 ? items[at - 1] : undefined;
  const next = at >= 0 ? items[at + 1] : undefined;
  const navigate = useNavigate();
  const { setOpenMobile } = useSidebar();

  // ← and → as on Udemy, unless the learner is typing
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey || editing(e.target)) return;
      const to = e.key === "ArrowLeft" ? prev : e.key === "ArrowRight" ? next : undefined;
      if (to) void navigate({ to: "/learn/$course/$item", params: { course: course.id, item: to.item.id } });
    }
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [prev, next, navigate, course.id]);

  return (
    <>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-10 flex items-center gap-3 border-b-4 border-ink bg-enamel px-4 py-2">
          <Link
            to="/courses/$course"
            params={{ course: course.id }}
            className="flex min-w-0 items-center gap-1 font-extrabold"
          >
            <ChevronLeft aria-hidden className="size-4 shrink-0" />
            <span className="truncate">{course.title}</span>
          </Link>
          <nav aria-label="Items" className="ml-auto flex items-center gap-2">
            <Step to={prev} course={course.id} dir="prev" />
            <Step to={next} course={course.id} dir="next" />
          </nav>
          <ThemeToggle />
          <ContentToggle />
        </header>
        <main id="main" className="flex-1 px-4 py-8 sm:px-8">
          {current ? <ItemView course={course} placedItem={current} /> : <Missing what="item" error="not-found" />}
          {next ? (
            <div className="mx-auto mt-12 flex max-w-[70ch] justify-end">
              <Button asChild>
                <Link to="/learn/$course/$item" params={{ course: course.id, item: next.item.id }}>
                  Next: {typeName[next.item.type]} {next.number} <ChevronRight aria-hidden />
                </Link>
              </Button>
            </div>
          ) : null}
        </main>
      </div>
      <Sidebar side="right">
        <SidebarHeader id="course-content" className="border-b border-rule px-4 py-3 font-extrabold">
          Course content
        </SidebarHeader>
        <SidebarContent role="navigation" aria-labelledby="course-content">
          {course.sections.map((s, si) => (
            <SidebarGroup key={s.title}>
              <SidebarGroupLabel className="text-sm font-extrabold text-ink">
                {si + 1} &nbsp;{s.title}
              </SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {items
                    .filter((p) => p.section === si)
                    .map(({ item, number }) => (
                      <SidebarMenuItem key={item.id}>
                        <SidebarMenuButton asChild isActive={item.id === itemId} className="h-auto py-2">
                          <Link
                            to="/learn/$course/$item"
                            params={{ course: course.id, item: item.id }}
                            aria-current={item.id === itemId ? "page" : undefined}
                            onClick={() => {
                              setOpenMobile(false); // the drawer (phones) closes on a choice
                            }}
                          >
                            <ItemIcon type={item.type} />
                            <span className="flex-1">
                              {number} {item.title}
                              <span className="block text-xs text-ink-soft">
                                {typeName[item.type]} · {duration(item.minutes)}
                              </span>
                            </span>
                          </Link>
                        </SidebarMenuButton>
                      </SidebarMenuItem>
                    ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          ))}
        </SidebarContent>
      </Sidebar>
    </>
  );
}

function Step({ to, course, dir }: { to: Placed | undefined; course: string; dir: "prev" | "next" }) {
  const label = dir === "prev" ? "Previous" : "Next";
  const icon = dir === "prev" ? <ChevronLeft aria-hidden /> : <ChevronRight aria-hidden />;
  if (!to)
    return (
      <Button variant="outline" size="sm" disabled className="bg-paper">
        {dir === "prev" ? icon : null}
        {label}
        {dir === "next" ? icon : null}
      </Button>
    );
  return (
    <Button asChild variant="outline" size="sm" className="bg-paper">
      <Link
        to="/learn/$course/$item"
        params={{ course, item: to.item.id }}
        aria-label={`${label}: ${to.number} ${to.item.title}`}
      >
        {dir === "prev" ? icon : null}
        <span className="hidden sm:inline">{label}</span>
        {dir === "next" ? icon : null}
      </Link>
    </Button>
  );
}

function ContentToggle() {
  const { toggleSidebar, open, openMobile, isMobile } = useSidebar();
  const shown = isMobile ? openMobile : open;
  return (
    <Button variant="outline" size="sm" className="bg-paper" onClick={toggleSidebar} aria-expanded={shown}>
      <ListTree aria-hidden />
      <span className="hidden sm:inline">Course content</span>
      <span className="sr-only sm:hidden">Course content</span>
    </Button>
  );
}

function ItemView({ course, placedItem }: { course: Course; placedItem: Placed }) {
  const item = useQuery(queries.item(course.id, placedItem.item.id));
  if (item.isPending)
    return <Skeleton aria-label="Loading the item" className="mx-auto h-96 max-w-[70ch] rounded-md" />;
  if (item.isError) return <Missing what="item" error={item.error} />;
  const props = { item: item.data, number: placedItem.number };
  switch (item.data.type) {
    case "lecture":
      return <LectureView {...props} courseDescription={course.description} />;
    case "quiz":
      return <QuizPreview {...props} />;
    case "lab":
      return <LabPreview {...props} />;
    case "challenge":
      return <ChallengePreview {...props} />;
  }
}
