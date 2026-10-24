import { expect, test } from "@playwright/test";

// M0 smoke: the binary serves the embedded app, the app reaches the API, client routes and themes work.

test("the shell loads and the platform lamp turns green", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveTitle("OpsForge Academy");
  await expect(page.getByRole("heading", { level: 1, name: "Courses" })).toBeVisible();
  const lamp = page.getByRole("status");
  await expect(lamp).toHaveText("Platform ready");
  await expect(lamp).toHaveAttribute("data-lamp", "green");
});

test("a deep link is answered by the app, an unknown page by its not-found view", async ({ page }) => {
  const response = await page.goto("/learn/no-course/no-item");
  expect(response?.status()).toBe(200);
  await expect(page.getByRole("heading", { name: "No such page" })).toBeVisible();
  await page.getByRole("link", { name: "Back to the courses" }).click();
  await expect(page).toHaveURL("/");
});

test("the theme toggle pins light, then dark, then follows the system again", async ({ page }) => {
  await page.goto("/");
  const html = page.locator("html");
  const toggle = page.getByRole("button", { name: /^Theme:/ });
  await toggle.click();
  await expect(html).toHaveAttribute("data-theme", "light");
  await toggle.click();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await page.getByRole("button", { name: /^Theme:/ }).click();
  await expect(html).not.toHaveAttribute("data-theme", /.+/);
});

test("the API answers problems for unknown resources", async ({ request }) => {
  const response = await request.get("/api/v1/nope");
  expect(response.status()).toBe(404);
  expect(response.headers()["content-type"]).toBe("application/problem+json");
  expect(await response.json()).toMatchObject({ type: "/problems/not-found", status: 404 });
});
