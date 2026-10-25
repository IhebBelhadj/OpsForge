import { createFileRoute } from "@tanstack/react-router";

import { queries } from "@/api/client";
import { CoursePage } from "@/features/course";

export const Route = createFileRoute("/courses/$course")({
  loader: ({ context, params }) => context.queryClient.query(queries.course(params.course)).catch(() => undefined),
  component: function Course() {
    const { course } = Route.useParams();
    return <CoursePage courseId={course} />;
  },
});
