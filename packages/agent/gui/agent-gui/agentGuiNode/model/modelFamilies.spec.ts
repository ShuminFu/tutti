import { describe, expect, it } from "vitest";
import {
  collapseModelOptionsToLatest,
  groupModelOptionsByVendor,
  modelVendorLabel,
  parseModelFamily
} from "./modelFamilies";

function option(value: string, label = value) {
  return { value, label };
}

describe("parseModelFamily", () => {
  it("splits claude by tier and keeps versions comparable", () => {
    expect(parseModelFamily(option("claude-opus-4-8"))).toEqual({
      family: "claude-opus",
      version: [4, 8]
    });
    expect(parseModelFamily(option("claude-sonnet-5"))).toEqual({
      family: "claude-sonnet",
      version: [5]
    });
    expect(parseModelFamily(option("claude-fable-5"))).toEqual({
      family: "claude-fable",
      version: [5]
    });
  });

  it("collapses every gpt variant into one family", () => {
    expect(parseModelFamily(option("gpt-5.2"))?.family).toBe("gpt");
    expect(parseModelFamily(option("gpt-5.3-codex"))?.family).toBe("gpt");
    expect(parseModelFamily(option("gpt-5.3-codex-low"))?.family).toBe("gpt");
    expect(parseModelFamily(option("gpt-5.3-codex"))?.version).toEqual([5, 3]);
  });

  it("keeps tier tokens that follow the version separate", () => {
    expect(parseModelFamily(option("gemini-3.1-pro"))).toEqual({
      family: "gemini-pro",
      version: [3, 1]
    });
    expect(parseModelFamily(option("gemini-3-flash"))).toEqual({
      family: "gemini-flash",
      version: [3]
    });
    expect(parseModelFamily(option("gemini-2.5-flash-lite"))).toEqual({
      family: "gemini-flash-lite",
      version: [2, 5]
    });
    expect(parseModelFamily(option("gemini-3-flash-preview"))?.family).toBe(
      "gemini-flash"
    );
  });

  it("drops variant and effort tokens from the family", () => {
    expect(parseModelFamily(option("kimi-k2.7-code"))).toEqual({
      family: "kimi",
      version: [2, 7]
    });
    expect(parseModelFamily(option("claude-sonnet-5-thinking"))?.family).toBe(
      "claude-sonnet"
    );
  });

  it("parses the parameterized value when the label is empty", () => {
    expect(
      parseModelFamily({
        value: "claude-sonnet-5[thinking=true,context=300k,effort=high]",
        label: ""
      })
    ).toEqual({ family: "claude-sonnet", version: [5] });
  });

  it("skips unversioned entries such as Auto", () => {
    expect(parseModelFamily({ value: "default[]", label: "Auto" })).toBeNull();
    expect(parseModelFamily(option("auto"))).toBeNull();
  });
});

describe("collapseModelOptionsToLatest", () => {
  it("keeps one latest entry per family and passes unversioned entries through", () => {
    const collapsed = collapseModelOptionsToLatest([
      { value: "default[]", label: "Auto" },
      option("composer-2.5"),
      option("claude-opus-4-1"),
      option("claude-opus-4-8"),
      option("claude-sonnet-4.5"),
      option("claude-sonnet-5"),
      option("gpt-5.2"),
      option("gpt-5.3-codex"),
      option("gemini-3.1-pro"),
      option("gemini-3-pro"),
      option("gemini-3-flash")
    ]);
    expect(collapsed.map((entry) => entry.value)).toEqual([
      "default[]",
      "composer-2.5",
      "claude-opus-4-8",
      "claude-sonnet-5",
      "gpt-5.3-codex",
      "gemini-3.1-pro",
      "gemini-3-flash"
    ]);
  });

  it("keeps a model and its 1M context row as separate entries", () => {
    // The marker is identity, not a run parameter: the collapsed list must
    // still offer both windows, so it must not tie-break the marker away.
    const collapsed = collapseModelOptionsToLatest([
      option("claude-opus-5"),
      option("claude-opus-5[1m]"),
      option("claude-sonnet-5"),
      option("claude-sonnet-5[1m]")
    ]);
    expect(collapsed.map((entry) => entry.value)).toEqual([
      "claude-opus-5",
      "claude-opus-5[1m]",
      "claude-sonnet-5",
      "claude-sonnet-5[1m]"
    ]);
  });

  it("still collapses other bracketed suffixes into their family", () => {
    // Only the context-window marker keys its own slot; a parameter suffix
    // keeps collapsing into the family it parameterizes.
    const collapsed = collapseModelOptionsToLatest([
      option("claude-opus-5"),
      option("claude-opus-5[fast=true]", "claude-opus-5")
    ]);
    expect(collapsed.map((entry) => entry.value)).toEqual(["claude-opus-5"]);
  });

  it("prefers the first-advertised option on version ties", () => {
    const collapsed = collapseModelOptionsToLatest([
      option("gpt-5.3-codex"),
      option("gpt-5.3-codex-low")
    ]);
    expect(collapsed.map((entry) => entry.value)).toEqual(["gpt-5.3-codex"]);
  });

  it("treats a longer version as newer than its prefix", () => {
    const collapsed = collapseModelOptionsToLatest([
      option("claude-haiku-4"),
      option("claude-haiku-4-5")
    ]);
    expect(collapsed.map((entry) => entry.value)).toEqual(["claude-haiku-4-5"]);
  });
});

describe("groupModelOptionsByVendor", () => {
  it("labels vendors from the first token", () => {
    expect(modelVendorLabel(option("claude-sonnet-5"))).toBe("Claude");
    expect(modelVendorLabel(option("gpt-5.3-codex"))).toBe("GPT");
    expect(modelVendorLabel(option("glm-5.2"))).toBe("GLM");
    expect(modelVendorLabel(option("gemini-3.1-pro"))).toBe("Gemini");
    expect(modelVendorLabel({ value: "default[]", label: "Auto" })).toBeNull();
  });

  it("buckets options by manufacturer with Auto leading ungrouped", () => {
    const groups = groupModelOptionsByVendor([
      { value: "default[]", label: "Auto" },
      option("composer-2.5"),
      option("claude-opus-4-8"),
      option("claude-sonnet-5"),
      option("gpt-5.3-codex"),
      option("gemini-3.1-pro"),
      option("gemini-3-flash")
    ]);
    expect(
      groups.map((group) => [
        group.label,
        group.options.map((entry) => entry.value)
      ])
    ).toEqual([
      [null, ["default[]"]],
      ["Composer", ["composer-2.5"]],
      ["Claude", ["claude-opus-4-8", "claude-sonnet-5"]],
      ["GPT", ["gpt-5.3-codex"]],
      ["Gemini", ["gemini-3.1-pro", "gemini-3-flash"]]
    ]);
  });
});
