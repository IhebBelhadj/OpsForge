import { createRootRouteWithContext, Link, Outlet, useRouterState } from "@tanstack/react-router";

import type { RouterContext } from "@/app/router";
import { ThemeToggle } from "@/app/theme-toggle";
import { StatusLamp } from "@/features/platform";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Shell,
  notFoundComponent: NotFound,
});

/** The app shell: a header rail, the page, a footer with the platform's lamp. The course player brings its own
 * chrome (design.md §3), so it gets the whole window. */
function Shell() {
  const player = useRouterState({ select: (s) => s.location.pathname.startsWith("/learn/") });
  if (player)
    return (
      <>
        <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:bg-paper focus:p-2">
          Skip to content
        </a>
        <Outlet />
      </>
    );
  return (
    <div className="flex min-h-svh flex-col">
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:p-2">
        Skip to content
      </a>
      <header className="border-b-4 border-ink bg-enamel">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-4 py-3">
          <Link to="/" className="font-extrabold tracking-wide text-ink uppercase">
            OpsForge <span className="font-semibold text-ink-soft">Academy</span>
          </Link>
          <ThemeToggle />
        </div>
      </header>
      <main id="main" className="mx-auto w-full max-w-6xl flex-1 px-4 py-10">
        <Outlet />
      </main>
      <footer className="border-t border-rule">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-4 py-3">
          <StatusLamp />
          <span className="text-sm text-ink-soft">127.0.0.1 only</span>
        </div>
      </footer>
    </div>
  );
}

function NotFound() {
  return (
    <section>
      <h1 className="font-serif text-3xl">No such page</h1>
      <p className="mt-3 text-ink-soft">
        <Link to="/" className="underline underline-offset-4">
          Back to the courses
        </Link>
      </p>
    </section>
  );
}
