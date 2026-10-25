import * as React from "react";
import { cn } from "@/lib/utils";
import { Progress as ProgressPrimitive } from "radix-ui";

// Restyled to the signal box: an enamel groove with an ink edge, filled green (passed) unless told otherwise
function Progress({
  className,
  indicatorClassName,
  value,
  ...props
}: React.ComponentProps<typeof ProgressPrimitive.Root> & { indicatorClassName?: string }) {
  return (
    <ProgressPrimitive.Root
      data-slot="progress"
      value={value}
      className={cn(
        "relative h-2.5 w-full overflow-hidden rounded-full border border-ink/60 bg-enamel-deep",
        className,
      )}
      {...props}
    >
      <ProgressPrimitive.Indicator
        data-slot="progress-indicator"
        className={cn(
          "h-full w-full flex-1 bg-green transition-transform motion-reduce:transition-none",
          indicatorClassName,
        )}
        style={{ transform: `translateX(-${String(100 - (value ?? 0))}%)` }}
      />
    </ProgressPrimitive.Root>
  );
}

export { Progress };
