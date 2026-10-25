import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { problems } from "@/mock";
import { renderRoute } from "@/test/render";

describe("practice", () => {
  it("lists everything to practise and filters by topic and search", async () => {
    const user = userEvent.setup();
    renderRoute("/practice");
    const table = await screen.findByRole("table");
    expect(within(table).getByRole("link", { name: "Losing a site" })).toBeInTheDocument();
    // Lose a cluster needs the multi-cluster layout: hidden while "Runs on this lab" is on
    expect(within(table).queryByRole("link", { name: "Lose a cluster" })).not.toBeInTheDocument();
    const chips = screen.getByRole("list", { name: "Topics" });
    await user.click(within(chips).getByRole("button", { name: "Services" }));
    const services = problems.filter((p) => p.topics.includes("Services")).length;
    expect(within(table).getAllByRole("row")).toHaveLength(services + 1); // and the header
    await user.click(within(chips).getByRole("button", { name: "All topics" }));
    await user.type(screen.getByRole("searchbox", { name: "Search practice" }), "rollout");
    expect(within(table).getByRole("link", { name: "Zero-downtime rollout" })).toBeInTheDocument();
    expect(within(table).getAllByRole("row")).toHaveLength(2);
  });

  it("counts what is solved per difficulty", async () => {
    renderRoute("/practice");
    const solved = await screen.findByRole("region", { name: "Solved" });
    const tasks = problems.filter((p) => p.type !== "quiz");
    const passed = tasks.filter((p) => p.status === "solved").length;
    expect(
      within(solved).getByText(`${String(passed)} of ${String(tasks.length)} labs and challenges passed`),
    ).toBeInTheDocument();
  });
});
