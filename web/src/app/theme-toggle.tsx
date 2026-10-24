import { Monitor, Moon, Sun } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";

type Theme = "system" | "light" | "dark";
const order: Theme[] = ["system", "light", "dark"];
const KEY = "opsforge-theme";

function stored(): Theme {
  try {
    const value = localStorage.getItem(KEY);
    return value === "light" || value === "dark" ? value : "system";
  } catch {
    return "system";
  }
}

/** Cycles the theme: follow the system, pinned light, pinned dark ("night shift"). Kept per browser. */
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(stored);
  useEffect(() => {
    if (theme === "system") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem(KEY, theme);
    } catch {
      // private window: the choice lasts for this page only
    }
  }, [theme]);
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
