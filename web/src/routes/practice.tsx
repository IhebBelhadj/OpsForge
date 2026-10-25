import { createFileRoute } from "@tanstack/react-router";

import { Practice } from "@/features/practice";

export const Route = createFileRoute("/practice")({
  component: Practice,
});
