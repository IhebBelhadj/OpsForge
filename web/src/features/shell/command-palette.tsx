import { useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { useEffect, useState } from "react";

import { ItemIcon } from "@/components/item-icon";
import { Button } from "@/components/ui/button";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import { lines, problems } from "@/mock";

import { pages } from "./pages";

/** Ctrl+K (design.md §6): jump to a page, a course or anything to practise. */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key.toLowerCase() === "k" && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        setOpen((o) => !o);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  function go(to: () => Promise<void>) {
    setOpen(false);
    void to();
  }

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="hidden w-52 justify-start border-ink/40 bg-paper text-ink-soft md:inline-flex"
        onClick={() => {
          setOpen(true);
        }}
      >
        <Search aria-hidden />
        Search
        <kbd className="ml-auto rounded-sm border border-rule px-1 font-sans text-xs">Ctrl K</kbd>
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="md:hidden"
        aria-label="Search"
        onClick={() => {
          setOpen(true);
        }}
      >
        <Search />
      </Button>
      <CommandDialog
        open={open}
        onOpenChange={setOpen}
        title="Search"
        description="Go to a page, a course or something to practise"
        className="border-2 border-ink bg-paper"
      >
        <CommandInput placeholder="Search courses, labs and challenges" />
        <CommandList>
          <CommandEmpty>Nothing matches. Try a topic, like “services”.</CommandEmpty>
          <CommandGroup heading="Pages">
            {pages.map((p) => (
              <CommandItem
                key={p.to}
                onSelect={() => {
                  go(() => navigate({ to: p.to }));
                }}
              >
                <p.icon aria-hidden />
                {p.name}
              </CommandItem>
            ))}
          </CommandGroup>
          <CommandGroup heading="Courses">
            {lines.map((l) => (
              <CommandItem
                key={l.course}
                onSelect={() => {
                  go(() => navigate({ to: "/roadmap", search: { course: l.course } }));
                }}
              >
                {l.title}
              </CommandItem>
            ))}
          </CommandGroup>
          <CommandGroup heading="Practice">
            {problems.map((p) => (
              <CommandItem
                key={p.id}
                value={`${p.title} ${p.topics.join(" ")}`}
                onSelect={() => {
                  go(() => navigate({ to: "/learn/$course/$item", params: p.to }));
                }}
              >
                <ItemIcon type={p.type} />
                {p.title}
                <CommandShortcut className="capitalize">{p.type}</CommandShortcut>
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </CommandDialog>
    </>
  );
}
