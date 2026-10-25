import { ExternalLink } from "lucide-react";

import type { Item } from "@/api/client";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { Markdown } from "./markdown";

/** A lecture in the player (design.md §3): the article, then Overview (the course's description) and Resources. */
export function LectureView({
  item,
  number,
  courseDescription,
}: {
  item: Item;
  number: string;
  courseDescription: string;
}) {
  const lecture = item.lecture;
  if (!lecture) return null;
  return (
    <div>
      <article aria-labelledby="item-title" className="mx-auto max-w-[70ch]">
        <p className="text-sm font-semibold tracking-wide text-ink-soft uppercase">
          Lecture {number} · {item.minutes} min
        </p>
        <h1 id="item-title" className="mt-1 font-serif text-3xl leading-tight sm:text-4xl">
          {item.title}
        </h1>
        <Markdown className="mt-6">{lecture.markdown}</Markdown>
      </article>
      <Tabs defaultValue="overview" className="mx-auto mt-12 max-w-[70ch]">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="resources">Resources ({lecture.resources.length})</TabsTrigger>
        </TabsList>
        <TabsContent value="overview" className="pt-4">
          <Markdown>{courseDescription}</Markdown>
        </TabsContent>
        <TabsContent value="resources" className="pt-4">
          {lecture.resources.length ? (
            <ul className="space-y-2">
              {lecture.resources.map((r) => (
                <li key={r.url}>
                  <a
                    href={r.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 underline decoration-rule underline-offset-4 hover:decoration-ink"
                  >
                    {r.title}
                    <ExternalLink aria-hidden className="size-3.5" />
                  </a>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-ink-soft">This lecture lists no resources.</p>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
