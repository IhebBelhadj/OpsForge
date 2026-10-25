import { Code, FileText, ListChecks, SquareTerminal } from "lucide-react";

import type { ItemType } from "@/api/client";
import { cn } from "@/lib/utils";

const icons = { lecture: FileText, quiz: ListChecks, lab: SquareTerminal, challenge: Code } as const;

/** The type of an item as an icon (decorative: the type is always written next to it). */
export function ItemIcon({ type, className }: { type: ItemType; className?: string }) {
  const Icon = icons[type];
  return <Icon aria-hidden className={cn("size-4 shrink-0", className)} />;
}
