import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";

import { highlight } from "./highlight";

/** A fenced code block: plain at once, coloured when Shiki has loaded; Copy puts the code on the clipboard. */
export function CodeBlock({ code, lang }: { code: string; lang: string }) {
  const [html, setHtml] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let live = true;
    if (lang)
      highlight(code, lang)
        .then((h) => {
          if (live) setHtml(h);
        })
        .catch(() => undefined); // stays plain
    return () => {
      live = false;
    };
  }, [code, lang]);

  async function copy() {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => {
      setCopied(false);
    }, 1500);
  }

  return (
    <div className="group relative my-5 rounded-md border border-rule bg-enamel-deep/40 font-mono text-[0.85rem]">
      {html ? (
        // Shiki's output: the code escaped into spans, nothing from the lecture as markup
        <div className="code-shiki overflow-x-auto p-4" dangerouslySetInnerHTML={{ __html: html }} />
      ) : (
        <pre className="overflow-x-auto p-4">
          <code>{code}</code>
        </pre>
      )}
      <Button
        variant="outline"
        size="sm"
        className="absolute top-2 right-2 h-7 gap-1 bg-paper px-2 text-xs"
        onClick={() => void copy()}
        aria-label={copied ? "Copied" : "Copy code"}
      >
        {copied ? <Check aria-hidden className="size-3.5" /> : <Copy aria-hidden className="size-3.5" />}
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}
