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

  it("takes a quiz: intro, one question at a time, then itemised results", async () => {
    const user = userEvent.setup();
    renderRoute("/learn/k8s-intro/pods-check");
    expect(await screen.findByText("Questions")).toBeInTheDocument();
    expect(screen.getByText("70 %")).toBeInTheDocument();
    expect(screen.getByText("No limit")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Start quiz" }));
    await user.click(screen.getByRole("radio", { name: "The kubelet, through the container runtime" }));
    expect(screen.getByRole("button", { name: "Question 1, answered" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Next/ }));
    expect(screen.getByText("Question 2 of 5")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Submit quiz" }));
    expect(screen.getByRole("heading", { name: "Not passed yet: 1 of 5" })).toBeInTheDocument();
    expect(screen.getAllByLabelText("Wrong")).toHaveLength(4);
    expect(screen.getByRole("button", { name: /Retake quiz/ })).toBeInTheDocument();
  });

  it("runs a lab: start, setup, a failed check with what was wanted, then the next step", async () => {
    const user = userEvent.setup();
    renderRoute("/learn/k8s-intro/meet-the-cluster");
    expect(await screen.findByRole("heading", { name: /List the nodes/ })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Start lab" }));
    expect(await screen.findByText("Connected", {}, { timeout: 4000 })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Check" }));
    const failed = await screen.findByRole("alert", {}, { timeout: 2000 });
    expect(within(failed).getByText("(none)")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Check" }));
    expect(await screen.findByText("Step 2 of 2", {}, { timeout: 2000 })).toBeInTheDocument();
  });

  it("solves a challenge: the starter fails, an edited manifest is accepted", async () => {
    const user = userEvent.setup();
    renderRoute("/learn/k8s-intro/first-pod");
    const editor = await screen.findByRole("textbox", { name: "Edit pod.yaml" });
    expect(editor).toHaveValue("apiVersion: v1\nkind: Pod\nmetadata:\n  name: web\n");
    expect(screen.getByText("Easy")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Submit" }));
    expect(await screen.findByText("Wrong answer", {}, { timeout: 2000 })).toBeInTheDocument();
    // jsdom lays everything out at 0×0, so the resizable panels take any click for a grab of their divider: focus the
    // editor directly
    editor.focus();
    await user.keyboard("spec: {{}}");
    await user.click(screen.getByRole("button", { name: "Submit" }));
    expect(await screen.findByText("Accepted", {}, { timeout: 2000 })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Solution" })).toBeInTheDocument();
  });

  it("says when the item is not in the course", async () => {
    renderRoute("/learn/k8s-intro/nope");
    expect(await screen.findByRole("heading", { name: "No such item" })).toBeInTheDocument();
  });
});
