import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { defineConfig, devices } from "@playwright/test";

// The e2e suite drives the real binary (make academy builds it with this web app embedded) against a fake lab
// toolchain and a throwaway database.
const port = 18090;
const db = join(mkdtempSync(join(tmpdir(), "academy-e2e-")), "academy.db");

export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: { baseURL: `http://127.0.0.1:${String(port)}`, trace: "retain-on-failure" },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "chromium-390", use: { ...devices["Desktop Chrome"], viewport: { width: 390, height: 844 } } },
    ...(process.env.CI ? [{ name: "firefox", use: { ...devices["Desktop Firefox"] } }] : []),
  ],
  webServer: {
    command: `../academy/bin/academy serve --addr 127.0.0.1:${String(port)} --root .. --db ${db} --toolchain ${join(import.meta.dirname, "e2e", "fake-opsforge.sh")} --log-level warn`,
    url: `http://127.0.0.1:${String(port)}/healthz`,
    reuseExistingServer: false,
    timeout: 20_000,
  },
});
