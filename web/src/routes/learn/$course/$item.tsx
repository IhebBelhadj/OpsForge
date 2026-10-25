import { createFileRoute } from "@tanstack/react-router";

import { queries } from "@/api/client";
import { Player } from "@/features/player";

export const Route = createFileRoute("/learn/$course/$item")({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.query(queries.course(params.course)).catch(() => undefined),
      context.queryClient.query(queries.item(params.course, params.item)).catch(() => undefined),
    ]),
  component: function Learn() {
    const { course, item } = Route.useParams();
    return <Player courseId={course} itemId={item} />;
  },
});
