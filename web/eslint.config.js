// ESLint (pnpm lint): typescript-eslint strict with types, React hooks, jsx-a11y strict, and feature boundaries:
// a feature is imported through its index.ts only (docs/courses/frontend.md "Structure").
import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default defineConfig(
  {
    ignores: ["dist", "src/routeTree.gen.ts", "src/api/schema.d.ts", "playwright-report", "test-results"],
  },
  js.configs.recommended,
  tseslint.configs.strictTypeChecked,
  tseslint.configs.stylisticTypeChecked,
  {
    languageOptions: {
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
      globals: globals.browser,
    },
  },
  reactHooks.configs.flat["recommended-latest"],
  jsxA11y.flatConfigs.strict,
  {
    files: ["src/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            { group: ["@/features/*/*"], message: "Import a feature through its index.ts (@/features/<name>)." },
            { group: ["../*/*"], message: "Use the @/ alias for imports outside this folder." },
          ],
        },
      ],
    },
  },
  {
    files: ["*.config.{js,ts}", "e2e/**"],
    languageOptions: { globals: globals.node },
  },
  {
    files: ["eslint.config.js"],
    extends: [tseslint.configs.disableTypeChecked],
  },
);
