import { resolveWorkspaceFilePathCandidate } from "../actions/workspaceLinkActions";

interface MarkdownNode {
  type: string;
  url?: string;
  value?: string;
  identifier?: string;
  depth?: number;
  children?: MarkdownNode[];
  data?: { hName?: string; hProperties?: Record<string, unknown> };
}

// Only deliverable files get cards; source references and inline mentions keep
// their existing navigation. The Markdown AST excludes examples in code fences.
const DELIVERABLE_EXTENSION =
  /\.(?:zip|tar|gz|7z|rar|pdf|docx?|xlsx?|pptx?|csv|png|jpe?g|gif|webp|svg|mp4|mp3|wav)$/i;

export function remarkAgentFileCards(options: {
  enabled: boolean;
  workspaceRoot?: string | null;
  basePath?: string | null;
}) {
  return (tree: MarkdownNode): void => {
    if (!options.enabled || !tree.children) return;
    const definitions = new Map<string, string>();
    const files = new Map<string, string>();
    const visit = (
      node: MarkdownNode,
      callback: (node: MarkdownNode) => void
    ) => {
      callback(node);
      node.children?.forEach((child) => visit(child, callback));
    };
    visit(tree, (node) => {
      if (node.type === "definition" && node.identifier && node.url) {
        definitions.set(node.identifier.toLowerCase(), node.url);
      }
    });
    visit(tree, (node) => {
      if (node.type !== "link" && node.type !== "linkReference") return;
      const href =
        node.url ?? definitions.get(node.identifier?.toLowerCase() ?? "");
      if (!href || node.children?.some((child) => child.type === "image"))
        return;
      if (node.children?.[0]?.value?.startsWith("@")) return;
      const path = resolveWorkspaceFilePathCandidate({
        path: href,
        ...options
      });
      if (!path || !DELIVERABLE_EXTENSION.test(path.path)) return;
      node.type = "link";
      node.url = path.path;
      files.set(path.path, path.path.split(/[\\/]/).at(-1) ?? path.path);
    });
    if (!files.size) return;

    const children = tree.children;
    for (const [path, name] of files) {
      children.push({
        type: "fileCard",
        data: {
          hName: "div",
          hProperties: { dataAgentFilePath: path, dataAgentFileName: name }
        },
        children: []
      });
    }
    tree.children = children;
  };
}
