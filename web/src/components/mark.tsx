import { cn } from "@/lib/utils";

/** OpsForge's mark: an anvil, in ember. Decorative: the wordmark beside it is the name. */
export function Mark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden focusable="false" className={cn("size-6 fill-ember", className)}>
      <path d="M1 5.5h15.5c2.6 0 4.7-.9 6.5-2.5v3.2c0 2.6-1.9 4.3-4.6 4.3h-1.9l-1 3.5H17V18H6.5v-4h1.6l-.8-3H5.2C2.9 11 1 9.4 1 7.2z" />
      <rect x="4" y="19.5" width="15.5" height="2.5" rx="0.6" />
    </svg>
  );
}
