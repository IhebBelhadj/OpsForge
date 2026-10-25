import { expect, test } from "@playwright/test";

// M0 smoke: the binary serves the embedded app, the app reaches the API, client routes and themes work.

test("the shell loads and the platform lamp turns green", async ({ page }) => {
  await page.goto("/courses");
  await expect(page).toHaveTitle("OpsForge Academy");
  await expect(page.getByRole("heading", { level: 1, name: "Courses" })).toBeVisible();
  const lamp = page.getByRole("status");
  await expect(lamp).toHaveText("Platform ready");
  await expect(lamp).toHaveAttribute("data-lamp", "green");
});

test("a deep link is answered by the app, an unknown page by its not-found view", async ({ page }) => {
  const response = await page.goto("/learn/no-course/no-item");
  expect(response?.status()).toBe(200);
  await expect(page.getByRole("heading", { name: "No such course" })).toBeVisible();
  await page.goto("/no/such/page");
  await expect(page.getByRole("heading", { name: "No such page" })).toBeVisible();
  await page.getByRole("link", { name: "Back to home" }).click();
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

test("the API needs a session, opened by POST /api/v1/sessions", async ({ request }) => {
  const anonymous = await request.get("/api/v1/me");
  expect(anonymous.status()).toBe(401);
  expect(await anonymous.json()).toMatchObject({ type: "/problems/no-session" });

  const opened = await request.post("/api/v1/sessions");
  expect(opened.status()).toBe(201);
  expect(opened.headers()["set-cookie"]).toMatch(/academy_session=.+; Path=\/; Max-Age=\d+; HttpOnly; SameSite=Strict/);
  const { csrf } = (await opened.json()) as { csrf: string };

  const me = await request.get("/api/v1/me");
  expect(me.status()).toBe(200);
  expect(await me.json()).toMatchObject({ user: { name: "learner" }, csrf });

  expect((await request.post("/api/v1/terminals")).status()).toBe(403);
  expect((await request.post("/api/v1/terminals", { headers: { "X-CSRF-Token": csrf } })).status()).toBe(201);
});

test("the API answers problems for unknown resources", async ({ request }) => {
  await request.post("/api/v1/sessions");
  const response = await request.get("/api/v1/nope");
  expect(response.status()).toBe(404);
  expect(response.headers()["content-type"]).toBe("application/problem+json");
  expect(await response.json()).toMatchObject({ type: "/problems/not-found", status: 404 });
});
