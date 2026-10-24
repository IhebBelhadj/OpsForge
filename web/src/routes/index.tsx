import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: Catalogue,
});

/** The catalogue. Empty until course packs are imported (Phase 11 M1-M2). */
function Catalogue() {
  return (
    <section aria-labelledby="catalogue">
      <h1 id="catalogue" className="font-serif text-4xl">
        Courses
      </h1>
      <p className="mt-4 max-w-prose text-lg text-ink-soft">
        No courses yet. Courses come from the packs in <code className="font-mono text-base">labs/</code>, once the
        platform imports them.
      </p>
    </section>
  );
}
