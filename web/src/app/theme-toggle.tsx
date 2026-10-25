import { Monitor, Moon, Sun } from "lucide-react";
import { useSyncExternalStore } from "react";

import { Button } from "@/components/ui/button";

export type Theme = "system" | "light" | "dark";
const order: Theme[] = ["system", "light", "dark"];
const KEY = "opsforge-theme";
const listeners = new Set<() => void>();

function stored(): Theme {
  try {
    const value = localStorage.getItem(KEY);
    return value === "light" || value === "dark" ? value : "system";
  } catch {
    return "system";
  }
}

let current: Theme = stored();
apply(current);

function apply(theme: Theme) {
  if (theme === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = theme;
}

/** Sets the theme everywhere (header toggle, settings) and keeps it per browser. */
export function setTheme(theme: Theme) {
  current = theme;
  apply(theme);
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // private window: the choice lasts for this page only
  }
  for (const l of listeners) l();
}

/** The theme: follow the system, pinned light, pinned dark ("night shift"). */
export function useTheme(): Theme {
  return useSyncExternalStore(
    (onChange) => {
      listeners.add(onChange);
      return () => listeners.delete(onChange);
    },
    () => current,
  );
}

/** Cycles the theme from the header. */
export function ThemeToggle() {
  const theme = useTheme();
  const next = order[(order.indexOf(theme) + 1) % order.length] ?? "system";
  const Icon = { system: Monitor, light: Sun, dark: Moon }[theme];
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={`Theme: ${theme}. Switch to ${next}`}
      onClick={() => {
        setTheme(next);
      }}
    >
      <Icon />
    </Button>
  );
}
