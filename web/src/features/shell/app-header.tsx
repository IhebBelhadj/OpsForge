import { Link } from "@tanstack/react-router";
import { Flame } from "lucide-react";

import { ThemeToggle } from "@/app/theme-toggle";
import { Mark } from "@/components/mark";
import { learner, levelFor } from "@/mock";

import { CommandPalette } from "./command-palette";
import { pages } from "./pages";

const nav = pages.filter((p) => p.to !== "/profile" && p.to !== "/settings");

const link =
  "relative flex items-center px-3 text-sm font-medium text-ink-soft transition-colors hover:text-ink data-[status=active]:text-ink data-[status=active]:after:absolute data-[status=active]:after:inset-x-3 data-[status=active]:after:bottom-0 data-[status=active]:after:h-0.5 data-[status=active]:after:bg-ink";

/** The top bar (Udemy's and LeetCode's): the mark and name, the four places, search, then the learner's streak,
 * level and the theme. */
export function AppHeader() {
  const { level } = levelFor(learner.xp);
  return (
    <header className="sticky top-0 z-30 border-b border-rule bg-surface">
      <div className="mx-auto flex h-14 max-w-7xl items-stretch gap-2 px-4 sm:px-6 md:gap-6">
        <Link to="/" className="flex items-center gap-2 text-[1.0625rem] font-semibold tracking-[-0.01em] text-ink">
          <Mark />
          OpsForge <span className="-ml-1 hidden font-normal text-ink-soft sm:inline">Academy</span>
        </Link>
        <nav aria-label="Main" className="hidden items-stretch md:flex">
          {nav.map((p) => (
            <Link key={p.to} to={p.to} activeOptions={{ exact: p.to === "/" }} className={link}>
              {p.name}
            </Link>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-1 sm:gap-2">
          <CommandPalette />
          <Link
            to="/profile"
            className="flex items-center gap-1 rounded-full px-2 py-1 text-sm font-semibold hover:bg-enamel"
            aria-label={`${String(learner.streak.current)} day streak, level ${String(level)}: your profile`}
          >
            <Flame
              aria-hidden
              className={learner.streak.today ? "size-4 fill-ember text-ember" : "size-4 text-lamp-off"}
            />
            {learner.streak.current}
            <span className="ml-2 grid size-8 place-items-center rounded-full bg-ink text-xs font-semibold text-surface">
              {level}
            </span>
          </Link>
          <ThemeToggle />
        </div>
      </div>
      <nav aria-label="Main" className="flex overflow-x-auto border-t border-rule px-2 md:hidden">
        {nav.map((p) => (
          <Link key={p.to} to={p.to} activeOptions={{ exact: p.to === "/" }} className={`${link} h-11 shrink-0`}>
            {p.name}
          </Link>
        ))}
      </nav>
    </header>
  );
}
