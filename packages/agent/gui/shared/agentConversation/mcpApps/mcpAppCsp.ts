import type { McpUiResourceCsp } from "./mcpAppProtocol";

// A declared origin is spliced into a CSP string, so anything outside a plain
// scheme://host[:port] shape (spaces, `;`, quotes, keywords such as `*` or
// 'unsafe-eval') could smuggle extra directives. Only accept bare origins,
// optionally with a leading `*.` subdomain wildcard as the spec allows.
const CSP_ORIGIN_PATTERN =
  /^(?:https|wss):\/\/(?:\*\.)?[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*(?::\d{1,5})?$/i;

export function sanitizeMcpAppCspOrigins(
  origins: readonly unknown[] | null | undefined
): string[] {
  if (!Array.isArray(origins)) {
    return [];
  }
  const accepted = new Set<string>();
  for (const origin of origins) {
    if (typeof origin !== "string") {
      continue;
    }
    const trimmed = origin.trim().replace(/\/+$/, "");
    if (CSP_ORIGIN_PATTERN.test(trimmed)) {
      accepted.add(trimmed);
    }
  }
  return [...accepted];
}

/**
 * Builds the policy for a display-only View.
 *
 * - `default-src 'none'` closes every fetch directive not listed below
 *   (object, media, worker, manifest…).
 * - Scripts/styles must be inline (the widget HTML) or come from the declared
 *   `resourceDomains` (the mainland-reachable CDN allowlist owned by the
 *   rndmaster MCP server). No `'self'`: a srcdoc iframe without
 *   `allow-same-origin` has an opaque origin, so `'self'` would mean nothing
 *   useful and would only widen the policy in some engines.
 * - `connect-src` stays `'none'` unless the resource declared connect
 *   origins, so `fetch`/XHR/WebSocket exfiltration is blocked by default.
 * - `frame-src`, `base-uri`, `form-action` are closed: no nested browsing
 *   contexts, no `<base>` rewriting of relative CDN URLs, no form posts.
 */
export function buildMcpAppContentSecurityPolicy(
  csp: McpUiResourceCsp | null | undefined
): string {
  const resourceDomains = sanitizeMcpAppCspOrigins(csp?.resourceDomains);
  const connectDomains = sanitizeMcpAppCspOrigins(csp?.connectDomains);
  const withResources = (...base: string[]): string =>
    [...base, ...resourceDomains].join(" ");
  return [
    "default-src 'none'",
    `script-src ${withResources("'unsafe-inline'")}`,
    `style-src ${withResources("'unsafe-inline'")}`,
    `img-src ${withResources("data:", "blob:")}`,
    `font-src ${withResources("data:")}`,
    `connect-src ${connectDomains.length > 0 ? connectDomains.join(" ") : "'none'"}`,
    "frame-src 'none'",
    "base-uri 'none'",
    "form-action 'none'"
  ].join("; ");
}

// Leading whitespace, comments and the doctype must stay in front of the
// injected tag: anything other than those before `<!doctype>` would drop the
// document into quirks mode.
const DOCUMENT_PREAMBLE_PATTERN =
  /^(?:\s|<!--[\s\S]*?-->)*(?:<!doctype[^>]*>)?/i;

/**
 * Places the CSP `<meta>` before any other markup of the resource.
 *
 * It is deliberately not inserted after a literal `<head>` match: a hostile
 * resource could put a `<script>` ahead of its `<head>` tag (or hide the text
 * `<head>` inside a script) and run before a later meta tag takes effect.
 * Emitting the meta directly after the doctype makes the HTML parser create
 * the head element for it, so it becomes the first child of `<head>` and is in
 * force before the resource's first byte of content is parsed. A later
 * `<html>`/`<head>` start tag only merges attributes into the existing
 * elements. Additional CSP metas inside the resource can only tighten it.
 */
export function injectMcpAppContentSecurityPolicy(
  html: string,
  policy: string
): string {
  const meta = `<meta http-equiv="Content-Security-Policy" content="${escapeHtmlAttribute(policy)}">`;
  const preamble = DOCUMENT_PREAMBLE_PATTERN.exec(html)?.[0] ?? "";
  return `${preamble}${meta}${html.slice(preamble.length)}`;
}

function escapeHtmlAttribute(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}
