import { useQuery } from "@tanstack/react-query";

import { api, keys, type Readiness } from "@/api/client";
import { cn } from "@/lib/utils";

type Lamp = "off" | "green" | "red";

const lampClass: Record<Lamp, string> = {
  off: "bg-lamp-off",
  green: "bg-green",
  red: "bg-red",
};

/** readyz, as a result either way: a 503 still carries the checks. Throws only when the platform does not answer. */
async function readiness(): Promise<Readiness> {
  const { data, error } = await api.GET("/readyz");
  return data ?? error;
}

/** The platform's own lamp: green when the database and the lab toolchain answer, red with the reason otherwise. */
export function StatusLamp() {
  const query = useQuery({ queryKey: keys.readiness, queryFn: readiness, refetchInterval: 15_000 });

  let lamp: Lamp = "off";
  let label = "Checking the platform…";
  let detail: string | undefined;
  if (query.isError) {
    lamp = "red";
    label = "Platform unreachable";
  } else if (query.data) {
    const failed = query.data.checks.filter((c) => !c.ok);
    lamp = failed.length ? "red" : "green";
    label = failed.length ? `${failed.map((c) => c.name).join(", ")} unavailable` : "Platform ready";
    detail = failed.map((c) => `${c.name}: ${c.detail ?? "failed"}`).join("; ") || undefined;
  }

  return (
    <span
      role="status"
      data-lamp={lamp}
      {...(detail ? { title: detail } : {})}
      className="inline-flex items-center gap-2 text-sm text-ink-soft"
    >
      <span aria-hidden className={cn("size-2 rounded-full", lampClass[lamp])} />
      {label}
    </span>
  );
}
