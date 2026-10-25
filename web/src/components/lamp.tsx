import { cn } from "@/lib/utils";

export type LampColour = "off" | "white" | "amber" | "red" | "green";

const colour: Record<LampColour, string> = {
  off: "bg-lamp-off",
  white: "bg-lamp",
  amber: "bg-amber shadow-[0_0_6px_var(--amber)]",
  red: "bg-red shadow-[0_0_6px_var(--red)]",
  green: "bg-green shadow-[0_0_6px_var(--green)]",
};

/** A panel lamp, the only colour on the page (white as configured, amber changed, red cut, green passed).
 * Decorative: what it means is always written next to it. */
export function Lamp({ on, className }: { on: LampColour; className?: string }) {
  return (
    <span
      aria-hidden
      data-lamp={on}
      className={cn("inline-block size-2.5 shrink-0 rounded-full border border-ink/50", colour[on], className)}
    />
  );
}
