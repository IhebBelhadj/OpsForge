import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderRoute } from "@/test/render";

describe("the profile", () => {
  it("shows the level, a year of practice and the badges", async () => {
    renderRoute("/profile");
    expect(await screen.findByText("Level 4")).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /days of practice in the last year/ })).toBeInTheDocument();
    expect(screen.getByText("Earned Oct 19, 2026")).toBeInTheDocument();
    expect(screen.getAllByText("Not earned yet")).toHaveLength(4);
  });
});
