import { Lamp } from "@/components/lamp";
import type { Difficulty } from "@/mock";

const lamp = { easy: "green", medium: "amber", hard: "red" } as const;
const name = { easy: "Easy", medium: "Medium", hard: "Hard" } as const;

/** LeetCode's difficulty label, in lamp colours (design.md §5b). */
export function DifficultyLabel({ level }: { level: Difficulty }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-sm font-semibold">
      <Lamp on={lamp[level]} />
      {name[level]}
    </span>
  );
}
