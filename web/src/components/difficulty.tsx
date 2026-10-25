import type { Difficulty } from "@/mock";
import { cn } from "@/lib/utils";

const tone = { easy: "text-easy", medium: "text-medium", hard: "text-hard" } as const;
const name = { easy: "Easy", medium: "Medium", hard: "Hard" } as const;

/** LeetCode's difficulty label: the word in its own colour. */
export function DifficultyLabel({ level, className }: { level: Difficulty; className?: string }) {
  return <span className={cn("text-sm font-medium", tone[level], className)}>{name[level]}</span>;
}
