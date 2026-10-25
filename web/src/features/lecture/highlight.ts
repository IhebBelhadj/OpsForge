import type { HighlighterCore } from "shiki/core";

/** Syntax colours for a code block, by Shiki (VS Code's grammars), loaded on first use so lectures without code
 * never fetch it. Only the languages lectures use are bundled (every Shiki language would add 13 MB to the binary),
 * with the JavaScript regex engine (no WASM). Both themes are in the output; app.css picks one by the page's theme.
 * Null for a language not listed (the block stays plain). */
const langs = {
  bash: () => import("shiki/langs/bash.mjs"),
  yaml: () => import("shiki/langs/yaml.mjs"),
  json: () => import("shiki/langs/json.mjs"),
  go: () => import("shiki/langs/go.mjs"),
  python: () => import("shiki/langs/python.mjs"),
  dockerfile: () => import("shiki/langs/dockerfile.mjs"),
  diff: () => import("shiki/langs/diff.mjs"),
  ini: () => import("shiki/langs/ini.mjs"),
};
const aliases: Record<string, keyof typeof langs> = {
  sh: "bash",
  shell: "bash",
  console: "bash",
  yml: "yaml",
  py: "python",
  toml: "ini",
};

let highlighter: Promise<HighlighterCore> | undefined;

function load() {
  highlighter ??= Promise.all([import("shiki/core"), import("shiki/engine/javascript")]).then(
    ([{ createHighlighterCore }, { createJavaScriptRegexEngine }]) =>
      createHighlighterCore({
        themes: [import("shiki/themes/github-light.mjs"), import("shiki/themes/github-dark.mjs")],
        langs: Object.values(langs).map((l) => l()),
        engine: createJavaScriptRegexEngine(),
      }),
  );
  return highlighter;
}

export async function highlight(code: string, lang: string): Promise<string | null> {
  const name = lang in langs ? lang : aliases[lang];
  if (!name) return null;
  const h = await load();
  return h.codeToHtml(code, {
    lang: name,
    themes: { light: "github-light", dark: "github-dark" },
    defaultColor: false,
  });
}
