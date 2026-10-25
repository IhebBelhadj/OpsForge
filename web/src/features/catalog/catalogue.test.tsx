import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { http, HttpResponse, server } from "@/test/server";
import { renderRoute } from "@/test/render";

describe("the catalogue", () => {
  it("shows each live course as a card linking to its page", async () => {
    renderRoute("/courses");
    const link = await screen.findByRole("link", { name: "Kubernetes introduction" });
    expect(link).toHaveAttribute("href", "/courses/k8s-intro");
    const card = link.closest("li") as HTMLElement;
    expect(within(card).getByText("Beginner")).toBeInTheDocument();
    expect(within(card).getByText("1 section, 1 lecture, 1 quiz, 1 lab, 1 challenge · 33 min")).toBeInTheDocument();
  });

  it("filters by level", async () => {
    const user = userEvent.setup();
    renderRoute("/courses");
    await screen.findByRole("link", { name: "Kubernetes introduction" });
    await user.click(screen.getByRole("combobox", { name: "Level" }));
    await user.click(await screen.findByRole("option", { name: "Advanced" }));
    expect(screen.queryByRole("link", { name: "Kubernetes introduction" })).not.toBeInTheDocument();
    expect(screen.getByText("No course at this level yet.")).toBeInTheDocument();
  });

  it("says where courses come from when there are none", async () => {
    server.use(http.get("*/api/v1/courses", () => HttpResponse.json({ courses: [] })));
    renderRoute("/courses");
    expect(await screen.findByText(/No courses yet/)).toBeInTheDocument();
  });
});
