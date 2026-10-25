import { cn } from "@/lib/utils";

/** The terminal's place in the layout until xterm.js is wired to /ws/terminal (frontend.md "Terminal"): the session's
 * output in the terminal colours, a cursor at the end. */
export function TerminalPane({ output, className }: { output: string; className?: string }) {
  return (
    <div
      role="log"
      aria-label="Terminal"
      className={cn("min-h-0 overflow-auto bg-term-bg p-4 font-mono text-[0.84rem] leading-6 text-term-fg", className)}
    >
      <pre className="whitespace-pre-wrap">
        {output}
        <span aria-hidden className="terminal-cursor inline-block h-[1.1em] w-[0.6em] translate-y-[0.2em] bg-term-fg" />
      </pre>
    </div>
  );
}
