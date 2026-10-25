import { type KeyboardEvent, useRef } from "react";

import { cn } from "@/lib/utils";

/** The editor's place in the layout until Monaco lands (frontend.md "Editor"): a monospace text area with line
 * numbers, Tab indenting two spaces. Same props Monaco's wrapper will take. */
export function CodeEditor({
  value,
  onChange,
  label,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  className?: string;
}) {
  const gutter = useRef<HTMLDivElement>(null);
  const lines = value.split("\n").length;

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key !== "Tab" || e.shiftKey) return;
    e.preventDefault();
    const t = e.currentTarget;
    const { selectionStart: a, selectionEnd: b } = t;
    onChange(`${value.slice(0, a)}  ${value.slice(b)}`);
    requestAnimationFrame(() => {
      t.selectionStart = t.selectionEnd = a + 2;
    });
  }

  return (
    <div className={cn("flex min-h-0 overflow-hidden bg-paper font-mono text-[0.84rem] leading-6", className)}>
      <div
        ref={gutter}
        aria-hidden
        className="shrink-0 overflow-hidden border-r border-rule bg-enamel/40 py-3 pr-3 pl-4 text-right text-ink-soft select-none"
      >
        {Array.from({ length: lines }, (_, i) => (
          <div key={i}>{i + 1}</div>
        ))}
      </div>
      <textarea
        aria-label={label}
        spellCheck={false}
        autoCapitalize="off"
        autoComplete="off"
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
        }}
        onKeyDown={onKeyDown}
        onScroll={(e) => {
          if (gutter.current) gutter.current.scrollTop = e.currentTarget.scrollTop;
        }}
        className="min-h-0 flex-1 resize-none bg-transparent px-4 py-3 whitespace-pre text-ink outline-none focus-visible:outline-none"
      />
    </div>
  );
}
