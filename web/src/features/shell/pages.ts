import { BookOpen, Dumbbell, House, Route, Settings, UserRound } from "lucide-react";

/** The app's top-level pages, in the header and the command palette. */
export const pages = [
  { to: "/", name: "Home", icon: House },
  { to: "/roadmap", name: "Roadmap", icon: Route },
  { to: "/courses", name: "Courses", icon: BookOpen },
  { to: "/practice", name: "Practice", icon: Dumbbell },
  { to: "/profile", name: "Profile", icon: UserRound },
  { to: "/settings", name: "Settings", icon: Settings },
] as const;
