import { useSyncExternalStore } from "react";

// design.md: below 900 px the player sidebar becomes a drawer
const query = "(max-width: 899px)";

function subscribe(onChange: () => void) {
  const mql = window.matchMedia(query);
  mql.addEventListener("change", onChange);
  return () => {
    mql.removeEventListener("change", onChange);
  };
}

/** Whether the window is narrower than the player's desktop layout. */
export function useIsMobile() {
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches);
}
