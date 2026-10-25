import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Markdown } from "./markdown";

vi.mock("./highlight", () => ({ highlight: () => Promise.resolve(null) }));

describe("Markdown", () => {
  it("renders GitHub-flavoured Markdown", () => {
    render(<Markdown>{"# Pods\n\n| Phase | Means |\n|---|---|\n| Running | started |\n"}</Markdown>);
    expect(screen.getByRole("heading", { name: "Pods" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "Running" })).toBeInTheDocument();
  });

  it("turns [!NOTE] blockquotes into labelled alerts", () => {
    render(<Markdown>{"> [!WARNING]\n> Deleting the namespace deletes its pods.\n"}</Markdown>);
    const alert = screen.getByRole("complementary", { name: "Warning" });
    expect(alert).toHaveTextContent("Deleting the namespace deletes its pods.");
    expect(alert).not.toHaveTextContent("[!WARNING]");
  });

  it("never renders raw HTML or script links from a pack", () => {
    const { container } = render(
      <Markdown>
        {'<script>alert(1)</script>\n\n<img src=x onerror="alert(1)">\n\n[click](javascript:alert(1))\n'}
      </Markdown>,
    );
    expect(container.querySelector("script, img[onerror]")).toBeNull();
    expect(screen.getByText("click").closest("a")?.getAttribute("href") ?? "").not.toMatch(/javascript/);
  });

  it("gives code blocks a Copy button that copies the code", async () => {
    const user = userEvent.setup();
    render(<Markdown>{"```bash\nopsforge kubectl get pods\n```\n"}</Markdown>);
    expect(screen.getByText("opsforge kubectl get pods")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Copy code" }));
    expect(await navigator.clipboard.readText()).toBe("opsforge kubectl get pods");
    expect(screen.getByRole("button", { name: "Copied" })).toBeInTheDocument();
  });

  it("names a pack image in its place (assets are not served yet)", () => {
    render(<Markdown>{"![A pod](../../../assets/pod.svg)\n"}</Markdown>);
    expect(screen.getByRole("img", { name: "A pod" })).toBeInTheDocument();
  });

  it("opens external links in a new tab", () => {
    render(<Markdown>{"[docs](https://kubernetes.io/)\n"}</Markdown>);
    expect(screen.getByRole("link", { name: "docs" })).toHaveAttribute("target", "_blank");
  });
});
