import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { renderRoute } from "@/test/render";

describe("the course page", () => {
  it("shows what the course teaches and starts at its first item", async () => {
    renderRoute("/courses/k8s-intro");
    expect(await screen.findByRole("heading", { level: 1, name: "Kubernetes introduction" })).toBeInTheDocument();
    const outcomes = screen.getByRole("region", { name: "What you'll learn" });
    expect(
      within(outcomes)
        .getAllByRole("listitem")
        .map((li) => li.textContent),
    ).toEqual(["Read a pod's status", "Write a pod manifest"]);
    expect(screen.getByRole("link", { name: "Start course" })).toHaveAttribute(
      "href",
      "/learn/k8s-intro/what-is-a-pod",
    );
    expect(screen.getByText("Needs: k3s")).toBeInTheDocument();
  });

  it("lists the curriculum by section, each item linking into the player", async () => {
    const user = userEvent.setup();
    renderRoute("/courses/k8s-intro");
    const content = await screen.findByRole("region", { name: "Course content" });
    const lab = within(content).getByRole("link", { name: /1\.3 Meet the cluster/ });
    expect(lab).toHaveAttribute("href", "/learn/k8s-intro/meet-the-cluster");
    expect(lab).toHaveTextContent("Lab");
    await user.click(within(content).getByRole("button", { name: /Pods/ }));
    expect(within(content).queryByRole("link", { name: /Meet the cluster/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Expand all" }));
    expect(within(content).getByRole("link", { name: /Meet the cluster/ })).toBeVisible();
  });

  it("renders the description as Markdown", async () => {
    renderRoute("/courses/k8s-intro");
    const description = await screen.findByRole("region", { name: "Description" });
    expect(within(description).getByText("tiny").tagName).toBe("STRONG");
  });

  it("says when the course does not exist", async () => {
    renderRoute("/courses/nope");
    expect(await screen.findByRole("heading", { name: "No such course" })).toBeInTheDocument();
  });
});
