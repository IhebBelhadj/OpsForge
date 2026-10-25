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
    rules: {
      "@typescript-eslint/no-unused-vars": [
        "error",
        { ignoreRestSiblings: true, argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
    },
  },
  {
    // shadcn's code, copied in (frontend.md "borrow, don't build"): kept close to upstream so updates stay diffable
    files: ["src/components/ui/**"],
    rules: {
      "@typescript-eslint/consistent-type-definitions": "off",
      "@typescript-eslint/restrict-template-expressions": "off",
      "@typescript-eslint/no-confusing-void-expression": "off",
      "react-hooks/purity": "off",
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
