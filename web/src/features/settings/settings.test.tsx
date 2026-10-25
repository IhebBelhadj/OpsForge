import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { setTheme } from "@/app/theme-toggle";
import { renderRoute } from "@/test/render";

afterEach(() => {
  setTheme("system");
});

describe("settings", () => {
  it("sets the theme for the whole page, the header's toggle included", async () => {
    const user = userEvent.setup();
    renderRoute("/settings");
    await user.click(await screen.findByRole("radio", { name: "Night shift" }));
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(screen.getByRole("button", { name: "Theme: dark. Switch to system" })).toBeInTheDocument();
  });
});
