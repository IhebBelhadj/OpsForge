import { cn } from "@/lib/utils";

export type LampColour = "off" | "white" | "amber" | "red" | "green";

const colour: Record<LampColour, string> = {
  off: "bg-lamp-off",
  white: "border border-control bg-lamp",
  amber: "bg-amber",
  red: "bg-red",
  green: "bg-green",
};

/** A status dot (white as configured, amber changed, red cut, green passed). Decorative: what it means is always
 * written next to it. */
export function Lamp({ on, className }: { on: LampColour; className?: string }) {
  return (
    <span
      aria-hidden
      data-lamp={on}
      className={cn("inline-block size-2 shrink-0 rounded-full", colour[on], className)}
    />
  );
}
