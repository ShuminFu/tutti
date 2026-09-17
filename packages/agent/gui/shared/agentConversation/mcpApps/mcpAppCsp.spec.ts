// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import {
  buildMcpAppContentSecurityPolicy,
  injectMcpAppContentSecurityPolicy,
  sanitizeMcpAppCspOrigins
} from "./mcpAppCsp";

const CDN_ALLOWLIST = [
  "https://lib.baomitu.com",
  "https://registry.npmmirror.com",
  "https://cdn.bootcdn.net",
  "https://cdn.staticfile.net"
];

function parse(html: string): Document {
  return new DOMParser().parseFromString(html, "text/html");
}

function cspMeta(document: Document): HTMLMetaElement | null {
  return document.querySelector('meta[http-equiv="Content-Security-Policy"]');
}

describe("buildMcpAppContentSecurityPolicy", () => {
  it("allows only inline code plus declared resource origins and blocks connections", () => {
    expect(
      buildMcpAppContentSecurityPolicy({
        resourceDomains: CDN_ALLOWLIST,
        connectDomains: []
      })
    ).toBe(
      [
        "default-src 'none'",
        `script-src 'unsafe-inline' ${CDN_ALLOWLIST.join(" ")}`,
        `style-src 'unsafe-inline' ${CDN_ALLOWLIST.join(" ")}`,
        `img-src data: blob: ${CDN_ALLOWLIST.join(" ")}`,
        `font-src data: ${CDN_ALLOWLIST.join(" ")}`,
        "connect-src 'none'",
        "frame-src 'none'",
        "base-uri 'none'",
        "form-action 'none'"
      ].join("; ")
    );
  });

  it("uses declared connect origins and the restrictive default without metadata", () => {
    expect(
      buildMcpAppContentSecurityPolicy({
        connectDomains: ["wss://realtime.example.com"]
      })
    ).toContain("connect-src wss://realtime.example.com;");
    expect(buildMcpAppContentSecurityPolicy(null)).toBe(
      "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'"
    );
  });

  it("drops origins that could inject directives or widen the policy", () => {
    expect(
      sanitizeMcpAppCspOrigins([
        "https://lib.baomitu.com/",
        "https://*.example.com",
        "https://cdn.example.com:8443",
        "https://x.com; connect-src *",
        "*",
        "'unsafe-eval'",
        "http://insecure.example.com",
        "data:",
        "https://evil.com/path",
        'https://a.com"><script>',
        42
      ])
    ).toEqual([
      "https://lib.baomitu.com",
      "https://*.example.com",
      "https://cdn.example.com:8443"
    ]);
    expect(
      buildMcpAppContentSecurityPolicy({
        resourceDomains: ["https://x.com; connect-src *"],
        connectDomains: ["*"]
      })
    ).toContain("connect-src 'none'");
  });
});

describe("injectMcpAppContentSecurityPolicy", () => {
  const policy = buildMcpAppContentSecurityPolicy({
    resourceDomains: CDN_ALLOWLIST
  });

  it("makes the CSP meta the first element of <head> for a full document", () => {
    const html =
      '<!DOCTYPE html>\n<html lang="en"><head><meta charset="utf-8"><title>Shell</title></head><body><div id="root"></div></body></html>';
    const injected = injectMcpAppContentSecurityPolicy(html, policy);

    expect(injected.startsWith("<!DOCTYPE html>")).toBe(true);
    const document = parse(injected);
    expect(document.compatMode).toBe("CSS1Compat");
    expect(document.head.firstElementChild).toBe(cspMeta(document));
    expect(cspMeta(document)?.getAttribute("content")).toBe(policy);
    expect(document.documentElement.getAttribute("lang")).toBe("en");
    expect(document.title).toBe("Shell");
    expect(document.getElementById("root")).not.toBeNull();
  });

  it("precedes a script placed ahead of the resource's own <head>", () => {
    const html =
      "<!doctype html><script>/* <head> */fetch('https://example.com')</script><html><head></head><body></body></html>";
    const document = parse(injectMcpAppContentSecurityPolicy(html, policy));
    const meta = cspMeta(document);
    const script = document.querySelector("script");

    expect(document.head.firstElementChild).toBe(meta);
    expect(meta && script).toBeTruthy();
    expect(
      meta!.compareDocumentPosition(script!) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
    // The script text was not split by the injection.
    expect(script!.textContent).toBe(
      "/* <head> */fetch('https://example.com')"
    );
  });

  it("handles fragments and leading comments", () => {
    const fragment = parse(
      injectMcpAppContentSecurityPolicy("<svg><rect/></svg>", policy)
    );
    expect(fragment.head.firstElementChild).toBe(cspMeta(fragment));

    const commented = injectMcpAppContentSecurityPolicy(
      "<!-- shell v1 -->\n<!doctype html><html><head></head></html>",
      policy
    );
    expect(commented.indexOf("<meta")).toBeGreaterThan(
      commented.toLowerCase().indexOf("<!doctype html>")
    );
    expect(parse(commented).compatMode).toBe("CSS1Compat");
  });
});
