import type { McpUiHostContext, McpUiTheme } from "./mcpAppProtocol";

/**
 * Standard MCP Apps style variable → the tutti renderer variable that plays
 * the same role. The iframe cannot see the host stylesheet (and `var(--x)`
 * references would not resolve inside it), so values are read as computed
 * strings from the element that hosts the frame. Reading from that element
 * rather than `:root` keeps the rndmaster `--rndmaster-host-*` palette bridge
 * (scoped to `.rndmaster-dintaldock-embedded` as well as the root) in effect.
 */
const STYLE_VARIABLE_SOURCES: ReadonlyArray<readonly [string, string]> = [
  ["--color-background-primary", "--background-fronted"],
  ["--color-background-secondary", "--background-panel"],
  ["--color-background-tertiary", "--background-soft"],
  ["--color-background-info", "--accent-bg"],
  ["--color-background-danger", "--on-danger"],
  ["--color-text-primary", "--text-primary"],
  ["--color-text-secondary", "--text-secondary"],
  ["--color-text-tertiary", "--text-tertiary"],
  ["--color-text-inverse", "--text-inverted"],
  ["--color-text-disabled", "--text-disabled"],
  ["--color-text-info", "--primary"],
  ["--color-text-danger", "--state-danger"],
  ["--color-text-success", "--state-success"],
  ["--color-text-warning", "--state-warning"],
  ["--color-border-primary", "--border"],
  ["--color-border-secondary", "--line-1"],
  ["--color-border-tertiary", "--line-2"],
  ["--color-ring-primary", "--ring"],
  ["--font-sans", "--font-ui"],
  ["--font-mono", "--font-mono-system"],
  ["--shadow-sm", "--shadow-soft"],
  ["--border-radius-md", "--radius"]
];

/** Fixed tokens with no renderer counterpart. */
const STATIC_STYLE_VARIABLES: Readonly<Record<string, string>> = {
  "--border-radius-xs": "4px",
  "--border-radius-sm": "6px",
  "--border-radius-lg": "14px",
  "--border-radius-full": "9999px",
  "--border-width-regular": "1px"
};

export function resolveMcpAppTheme(documentRef: Document): McpUiTheme {
  const documentTheme = documentRef.documentElement.dataset.theme;
  if (documentTheme === "dark" || documentTheme === "light") {
    return documentTheme;
  }
  const view = documentRef.defaultView;
  return typeof view?.matchMedia === "function" &&
    view.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

export function readMcpAppStyleVariables(
  element: Element
): Record<string, string> {
  const view = element.ownerDocument.defaultView;
  const computed = view?.getComputedStyle(element);
  // Root fallback for engines without custom-property inheritance in
  // getComputedStyle (jsdom); browsers already resolve it on the element.
  const rootComputed = view?.getComputedStyle(
    element.ownerDocument.documentElement
  );
  const variables: Record<string, string> = { ...STATIC_STYLE_VARIABLES };
  if (!computed) {
    return variables;
  }
  for (const [standardName, sourceName] of STYLE_VARIABLE_SOURCES) {
    const value =
      computed.getPropertyValue(sourceName).trim() ||
      rootComputed?.getPropertyValue(sourceName).trim();
    if (value) {
      variables[standardName] = value;
    }
  }
  return variables;
}

/** Theme-dependent part of the host context; compared to detect changes. */
export function readMcpAppThemeContext(
  element: Element
): Pick<McpUiHostContext, "theme" | "styles"> {
  return {
    theme: resolveMcpAppTheme(element.ownerDocument),
    styles: { variables: readMcpAppStyleVariables(element) }
  };
}

export function sameMcpAppThemeContext(
  left: Pick<McpUiHostContext, "theme" | "styles">,
  right: Pick<McpUiHostContext, "theme" | "styles">
): boolean {
  if (left.theme !== right.theme) {
    return false;
  }
  const leftVariables = left.styles?.variables ?? {};
  const rightVariables = right.styles?.variables ?? {};
  const leftKeys = Object.keys(leftVariables);
  return (
    leftKeys.length === Object.keys(rightVariables).length &&
    leftKeys.every((key) => leftVariables[key] === rightVariables[key])
  );
}

/**
 * Subscribes to everything that can change the resolved theme: the root
 * `data-theme` / host palette attributes and inline token styles, plus the OS
 * color-scheme when no explicit theme is set.
 */
export function observeMcpAppThemeSources(
  documentRef: Document,
  onChange: () => void
): () => void {
  const observer = new MutationObserver(onChange);
  observer.observe(documentRef.documentElement, {
    attributes: true,
    attributeFilter: [
      "data-theme",
      "data-rndmaster-host-theme",
      "class",
      "style"
    ]
  });
  const media =
    typeof documentRef.defaultView?.matchMedia === "function"
      ? documentRef.defaultView.matchMedia("(prefers-color-scheme: dark)")
      : null;
  media?.addEventListener?.("change", onChange);
  return () => {
    observer.disconnect();
    media?.removeEventListener?.("change", onChange);
  };
}
