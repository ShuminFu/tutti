import { describe, expect, it } from "vitest";
import { agentGUIProviderRailIconPresentation } from "./agent-gui/agentGuiNode/view/AgentGUIEmptyState";

describe("agentGUIProviderRailIconPresentation", () => {
  it("prefers the target sidebar icon over its canonical icon", () => {
    expect(
      agentGUIProviderRailIconPresentation(
        "acp:example",
        "app://example/icon.svg",
        "app://example/sidebar-icon.svg"
      ).iconUrl
    ).toBe("app://example/sidebar-icon.svg");
  });

  it("keeps the canonical icon fallback for targets without sidebar art", () => {
    expect(
      agentGUIProviderRailIconPresentation(
        "acp:example",
        "app://example/icon.svg"
      ).iconUrl
    ).toBe("app://example/icon.svg");
  });
});
