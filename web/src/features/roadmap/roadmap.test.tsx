import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderRoute } from "@/test/render";

describe("the roadmap", () => {
  it("opens on the station you are at and shows another when picked", async () => {
    const user = userEvent.setup();
    renderRoute("/roadmap");
    const panel = await screen.findByRole("complementary");
    expect(within(panel).getByRole("heading", { name: "Pods" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "3. Pods, you are here" })).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "2. Resources, locked" }));
    expect(within(panel).getByRole("heading", { name: "Resources" })).toBeInTheDocument();
    expect(within(panel).getByText(/Opens after Kubernetes Fundamentals/)).toBeInTheDocument();
  });

  it("starts on a course's first station from a link", async () => {
    renderRoute("/roadmap?course=production");
    const panel = await screen.findByRole("complementary");
    expect(within(panel).getByRole("heading", { name: "Access control" })).toBeInTheDocument();
  });
});
