import { Link } from "@tanstack/react-router";
import { Flame } from "lucide-react";

import { ThemeToggle } from "@/app/theme-toggle";
import { learner, levelFor } from "@/mock";

import { CommandPalette } from "./command-palette";
import { pages } from "./pages";

const nav = pages.filter((p) => p.to !== "/profile" && p.to !== "/settings");

/** The header rail: the wordmark, the four places, then the learner's streak, level and the palette. */
export function AppHeader() {
  const { level } = levelFor(learner.xp);
  return (
    <header className="border-b-4 border-ink bg-enamel">
      <div className="mx-auto flex max-w-7xl items-stretch gap-3 px-4 md:gap-6">
        <Link to="/" className="flex items-center py-3 font-extrabold tracking-wide text-ink uppercase">
          OpsForge <span className="ml-1.5 hidden font-semibold text-ink-soft sm:inline">Academy</span>
        </Link>
        <nav aria-label="Main" className="hidden items-stretch gap-1 md:flex">
          {nav.map((p) => (
            <Link
              key={p.to}
              to={p.to}
              activeOptions={{ exact: p.to === "/" }}
              className="relative flex items-center px-3 text-ink-soft hover:text-ink data-[status=active]:font-extrabold data-[status=active]:text-ink data-[status=active]:after:absolute data-[status=active]:after:inset-x-2 data-[status=active]:after:-bottom-1 data-[status=active]:after:h-1 data-[status=active]:after:bg-ink"
            >
              {p.name}
            </Link>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-1 sm:gap-2">
          <CommandPalette />
          <Link
            to="/profile"
            className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm font-semibold hover:bg-enamel-deep"
            aria-label={`${String(learner.streak.current)} day streak, level ${String(level)}: your profile`}
          >
            <Flame aria-hidden className={learner.streak.today ? "size-4 text-amber" : "size-4 text-lamp-off"} />
            {learner.streak.current}
            <span className="ml-2 grid size-7 place-items-center rounded-full border-2 border-ink bg-paper text-xs font-extrabold">
              {level}
            </span>
          </Link>
          <ThemeToggle />
        </div>
      </div>
      <nav aria-label="Main" className="flex gap-1 overflow-x-auto border-t border-ink/30 px-2 md:hidden">
        {nav.map((p) => (
          <Link
            key={p.to}
            to={p.to}
            activeOptions={{ exact: p.to === "/" }}
            className="shrink-0 px-3 py-2 text-sm text-ink-soft data-[status=active]:font-extrabold data-[status=active]:text-ink"
          >
            {p.name}
          </Link>
        ))}
      </nav>
    </header>
  );
}
