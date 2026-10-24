import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// `pnpm dev` proxies the platform's routes to a running `academy serve` (ACADEMY_URL, default its usual address).
const academy = process.env.ACADEMY_URL ?? "http://127.0.0.1:8090";

export default defineConfig({
  plugins: [
    // Before react(): generates src/routeTree.gen.ts from src/routes/ and splits each route into its own chunk
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
  ],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    proxy: {
      "/api": academy,
      "/healthz": academy,
      "/readyz": academy,
      "/ws": { target: academy, ws: true },
    },
  },
  build: { outDir: "dist", emptyOutDir: true },
});
