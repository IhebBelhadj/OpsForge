import { ImageOff, Info, Lightbulb, MessageSquareWarning, OctagonAlert, TriangleAlert } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import remarkGfm from "remark-gfm";

import { cn } from "@/lib/utils";

import { type Alert, ALERTS, remarkAlerts } from "./alerts";
import { CodeBlock } from "./code-block";

// No raw HTML from packs (architecture.md "Security"): react-markdown never parses it, and the sanitizer drops
// anything the default (GitHub's) schema does not allow; alerts add one attribute.
const schema = {
  ...defaultSchema,
  attributes: { ...defaultSchema.attributes, blockquote: [["dataAlert", ...ALERTS]] },
};

const alertStyle: Record<Alert, { label: string; icon: ReactNode; className: string }> = {
  note: { label: "Note", icon: <Info />, className: "border-ink-soft" },
  tip: { label: "Tip", icon: <Lightbulb />, className: "border-green" },
  important: { label: "Important", icon: <MessageSquareWarning />, className: "border-ink-soft" },
  warning: { label: "Warning", icon: <TriangleAlert />, className: "border-amber" },
  caution: { label: "Caution", icon: <OctagonAlert />, className: "border-red" },
};

interface HastText {
  type: string;
  value?: string;
  children?: HastText[];
  properties?: { className?: unknown };
}

function text(node: HastText | undefined): string {
  if (!node) return "";
  return node.value ?? (node.children ?? []).map(text).join("");
}

const components: Components = {
  pre({ node }) {
    const code = node?.children[0] as HastText | undefined;
    const classes = Array.isArray(code?.properties?.className) ? (code.properties.className as string[]) : [];
    const lang = classes.find((c) => c.startsWith("language-"))?.slice("language-".length) ?? "";
    return <CodeBlock code={text(code).replace(/\n$/, "")} lang={lang} />;
  },
  blockquote({ node: _node, children, ...props }) {
    const kind = (props as Record<string, unknown>)["data-alert"] as Alert | undefined;
    if (!kind) return <blockquote className="my-5 border-l-4 border-rule pl-4 text-ink-soft">{children}</blockquote>;
    const style = alertStyle[kind];
    return (
      <aside
        aria-label={style.label}
        className={cn("my-5 rounded-r-md border-l-4 bg-enamel/50 py-3 pr-4 pl-4 font-sans", style.className)}
      >
        <p className="mb-1 flex items-center gap-2 text-sm font-semibold [&_svg]:size-4" aria-hidden>
          {style.icon}
          {style.label}
        </p>
        <div className="[&>p]:my-0">{children}</div>
      </aside>
    );
  },
  // Lecture images live in the pack's assets/; serving them is not built yet, so the image is named in its place
  img({ alt }) {
    return (
      <span
        role="img"
        aria-label={alt ?? "image"}
        className="my-5 flex items-center gap-2 rounded-md border border-dashed border-rule p-4 font-sans text-sm text-ink-soft"
      >
        <ImageOff aria-hidden className="size-4 shrink-0" />
        {alt ?? "image"}
      </span>
    );
  },
  a({ node: _node, href, children, ...props }) {
    const external = /^https?:/.test(href ?? "");
    return (
      <a
        {...props}
        href={href}
        className="underline decoration-rule underline-offset-4 hover:decoration-ink"
        {...(external ? { target: "_blank", rel: "noreferrer" } : {})}
      >
        {children}
      </a>
    );
  },
  code({ node: _node, children, ...props }) {
    return (
      <code {...props} className="rounded-sm bg-enamel px-1 py-0.5 font-mono text-[0.85em]">
        {children}
      </code>
    );
  },
  table({ node: _node, children }) {
    return (
      <div className="my-5 overflow-x-auto">
        <table className="w-full border-collapse font-sans text-sm [&_td]:border [&_td]:border-rule [&_td]:px-3 [&_td]:py-1.5 [&_th]:border [&_th]:border-rule [&_th]:bg-enamel [&_th]:px-3 [&_th]:py-1.5 [&_th]:text-left">
          {children}
        </table>
      </div>
    );
  },
};

/** Pack Markdown as reading text: GitHub-flavoured (tables, task lists), alerts, highlighted code with Copy. */
export function Markdown({ children, className }: { children: string } & Pick<ComponentProps<"div">, "className">) {
  return (
    <div className={cn("prose-lecture", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkAlerts]}
        rehypePlugins={[[rehypeSanitize, schema]]}
        components={components}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}
