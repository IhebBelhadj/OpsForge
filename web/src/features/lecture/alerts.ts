/** GitHub-style alerts (content-model.md): a blockquote whose text starts with [!NOTE], [!TIP], [!IMPORTANT],
 * [!WARNING] or [!CAUTION] loses the marker and carries data-alert, which the renderer turns into an alert. */
export const ALERTS = ["note", "tip", "important", "warning", "caution"] as const;
export type Alert = (typeof ALERTS)[number];

interface MdNode {
  type: string;
  value?: string;
  children?: MdNode[];
  data?: { hProperties?: Record<string, unknown> };
}

const marker = /^\[!(note|tip|important|warning|caution)\][ \t]*\n?/i;

function walk(node: MdNode) {
  if (node.type === "blockquote") {
    const text = node.children?.[0]?.children?.[0];
    const m = text?.type === "text" ? marker.exec(text.value ?? "") : null;
    if (text && m?.[1]) {
      text.value = (text.value ?? "").slice(m[0].length);
      node.data = { ...node.data, hProperties: { dataAlert: m[1].toLowerCase() } };
    }
  }
  node.children?.forEach(walk);
}

export function remarkAlerts() {
  return walk;
}
