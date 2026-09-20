import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const themeCss = readFileSync("src/styles/theme.css", "utf8");
const baseCss = readFileSync("src/styles/base.css", "utf8");

describe("text selection", () => {
  it("keeps neutral selection tokens in light, explicit dark and system dark modes", () => {
    const backgrounds = [
      ...themeCss.matchAll(/--selection-background:\s*(#[a-f\d]+);/gi)
    ].map((match) => match[1]);
    const foregrounds = [
      ...themeCss.matchAll(/--selection-foreground:\s*(#[a-f\d]+);/gi)
    ].map((match) => match[1]);
    expect(backgrounds).toEqual(["#dfdfdf", "#454545", "#454545"]);
    expect(foregrounds).toEqual(["#1a1c1f", "#f5f5f5", "#f5f5f5"]);
    expect(baseCss).toContain("::selection");
    expect(baseCss).toContain("background-color: var(--selection-background)");
    expect(baseCss).toContain("color: var(--selection-foreground)");
  });
});

function zIndexToken(name: string): number {
  const match = themeCss.match(new RegExp(`--${name}:\\s*(\\d+);`));
  if (!match?.[1]) {
    throw new Error(`Missing z-index token: --${name}`);
  }
  return Number(match[1]);
}

describe("global overlay layers", () => {
  it("keeps toast notifications above dialogs and below tooltips", () => {
    expect(zIndexToken("z-toast")).toBeGreaterThan(
      zIndexToken("z-dialog-popover")
    );
    expect(zIndexToken("z-toast")).toBeLessThan(zIndexToken("z-tooltip"));
  });
});
