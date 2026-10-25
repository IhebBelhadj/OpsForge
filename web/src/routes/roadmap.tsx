import { createFileRoute } from "@tanstack/react-router";

import { Roadmap } from "@/features/roadmap";

export const Route = createFileRoute("/roadmap")({
  validateSearch: (search: Record<string, unknown>): { course?: string } =>
    typeof search.course === "string" ? { course: search.course } : {},
  component: function RoadmapRoute() {
    const { course } = Route.useSearch();
    return <Roadmap key={course} course={course} />;
  },
});
