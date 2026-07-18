import { describe, expect, it } from "vitest";

import {
  agentGUIBuildEntries,
  agentGUIDtsEntryGroups
} from "./build/agentGuiBuildEntries";

describe("Agent GUI declaration build groups", () => {
  it("cover every runtime entry exactly once", () => {
    const declarationEntries = agentGUIDtsEntryGroups.flat();
    const runtimeEntries = Object.keys(agentGUIBuildEntries).sort();

    expect(new Set(declarationEntries).size).toBe(declarationEntries.length);
    expect([...declarationEntries].sort()).toEqual(runtimeEntries);
  });
});
