import { FileCode } from "lucide-react";

import { cn } from "@/lib/utils";

/** VS Code's tab strip over the editor: one tab per open file, a dot while unsaved. */
export function FileTabs({
  files,
  active,
  dirty,
  onPick,
  children,
}: {
  files: string[];
  active: string;
  dirty?: Set<string>;
  onPick: (file: string) => void;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex items-stretch border-b border-rule bg-enamel">
      <div role="tablist" aria-label="Open files" className="flex min-w-0 overflow-x-auto">
        {files.map((f) => (
          <button
            key={f}
            type="button"
            role="tab"
            aria-selected={f === active}
            onClick={() => {
              onPick(f);
            }}
            className={cn(
              "flex shrink-0 items-center gap-2 border-r border-rule px-3 py-1.5 font-mono text-xs",
              f === active
                ? "-mb-0.5 border-b-2 border-b-surface bg-surface font-semibold"
                : "text-ink-soft hover:text-ink",
            )}
          >
            <FileCode aria-hidden className="size-3.5" />
            {f}
            {dirty?.has(f) ? <span aria-label="unsaved" className="size-2 rounded-full bg-ink" /> : null}
          </button>
        ))}
      </div>
      <div className="ml-auto flex items-center gap-2 px-2 text-xs text-ink-soft">{children}</div>
    </div>
  );
}
