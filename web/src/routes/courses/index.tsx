import { createFileRoute } from "@tanstack/react-router";

import { queries } from "@/api/client";
import { Catalogue } from "@/features/catalog";

export const Route = createFileRoute("/courses/")({
  loader: ({ context }) => context.queryClient.query(queries.courses()).catch(() => undefined),
  component: Catalogue,
});
