import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderRoute } from "@/test/render";

describe("home", () => {
  it("resumes the course, shows the streak and what is next", async () => {
    renderRoute("/");
    expect(await screen.findByRole("heading", { level: 1, name: "Kubernetes Fundamentals" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Resume quiz" })).toHaveAttribute("href", "/learn/k8s-intro/what-is-a-pod");
    expect(screen.getByRole("heading", { name: "12 days in a row" })).toBeInTheDocument();
    const next = screen.getByRole("region", { name: "Next on your line" });
    expect(within(next).getByRole("link", { name: "Debug a crashing pod" })).toBeInTheDocument();
  });

  it("puts the main places in the header", async () => {
    renderRoute("/");
    const [nav = document.body] = await screen.findAllByRole("navigation", { name: "Main" });
    for (const name of ["Home", "Roadmap", "Courses", "Practice"])
      expect(within(nav).getByRole("link", { name })).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Home" })).toHaveAttribute("data-status", "active");
  });
});
