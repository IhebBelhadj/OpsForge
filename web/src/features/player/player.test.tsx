import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { renderRoute } from "@/test/render";

vi.mock("@/features/lecture/highlight", () => ({ highlight: () => Promise.resolve(null) }));

describe("the course player", () => {
  it("plays a lecture with its number, text and resources", async () => {
    const user = userEvent.setup();
    renderRoute("/learn/k8s-intro/what-is-a-pod");
    expect(await screen.findByRole("heading", { level: 1, name: "What is a pod" })).toBeInTheDocument();
    expect(screen.getByText("Lecture 1.1 · 8 min")).toBeInTheDocument();
    expect(screen.getByText(/share a network namespace/)).toBeInTheDocument();
    expect(screen.getByRole("complementary", { name: "Note" })).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Resources (1)" }));
    expect(screen.getByRole("link", { name: "Kubernetes docs: Pods" })).toHaveAttribute("target", "_blank");
  });

  it("marks the current item in the course content and links the others", async () => {
    renderRoute("/learn/k8s-intro/pods-check");
    const sidebar = await screen.findByRole("navigation", { name: "Course content" });
    const current = await within(sidebar).findByRole("link", { current: "page" });
    expect(current).toHaveTextContent("1.2 Pods check");
    expect(within(sidebar).getByRole("link", { name: /1\.4 First pod/ })).toHaveAttribute(
      "href",
      "/learn/k8s-intro/first-pod",
    );
  });

  it("steps through the course with Previous and Next, and with the arrow keys", async () => {
    const user = userEvent.setup();
    const { router } = renderRoute("/learn/k8s-intro/what-is-a-pod");
    expect(await screen.findByRole("button", { name: "Previous" })).toBeDisabled();
    await user.click(screen.getByRole("link", { name: "Next: 1.2 Pods check" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Pods check" })).toBeInTheDocument();
    await user.keyboard("{ArrowRight}");
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/learn/k8s-intro/meet-the-cluster");
    });
    await user.keyboard("{ArrowLeft}");
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/learn/k8s-intro/pods-check");
    });
  });

  it("previews a quiz by what an attempt will be, not its questions", async () => {
    renderRoute("/learn/k8s-intro/pods-check");
    expect(await screen.findByText("Questions")).toBeInTheDocument();
    expect(screen.getByText("70 %")).toBeInTheDocument();
    expect(screen.getByText("No limit")).toBeInTheDocument();
  });

  it("previews a lab's steps and how each is checked", async () => {
    renderRoute("/learn/k8s-intro/meet-the-cluster");
    expect(await screen.findByRole("heading", { name: /List the nodes/ })).toBeInTheDocument();
    expect(screen.getByText("Checked on the lab · 1 hint")).toBeInTheDocument();
    expect(screen.getByText("You confirm")).toBeInTheDocument();
  });

  it("previews a challenge with its test cases and starter file", async () => {
    renderRoute("/learn/k8s-intro/first-pod");
    expect(await screen.findByText("pod is ready")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "pod.yaml" })).toBeInTheDocument();
    expect(screen.getByText("easy")).toBeInTheDocument();
  });

  it("says when the item is not in the course", async () => {
    renderRoute("/learn/k8s-intro/nope");
    expect(await screen.findByRole("heading", { name: "No such item" })).toBeInTheDocument();
  });
});
