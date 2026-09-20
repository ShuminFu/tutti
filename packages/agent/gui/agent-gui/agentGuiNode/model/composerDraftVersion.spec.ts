import { describe, expect, it } from "vitest";
import {
  consumeComposerSubmittedRevision,
  createComposerDraftVersionTracker,
  recordComposerLocalDraftEdit,
  shouldApplyComposerDraftProp
} from "./composerDraftVersion";

describe("composer draft versions", () => {
  it("ignores a stale prefix echo after a newer local edit", () => {
    const tracker = createComposerDraftVersionTracker("session:one");
    recordComposerLocalDraftEdit(tracker, "A");
    recordComposerLocalDraftEdit(tracker, "AB");

    expect(
      shouldApplyComposerDraftProp({
        prompt: "A",
        revision: 1,
        scopeKey: "session:one",
        tracker
      })
    ).toBe(false);
    expect(tracker.nextRevision).toBe(2);
    expect(tracker.pendingLocalEchoes.map((echo) => echo.prompt)).toEqual([
      "AB"
    ]);
  });

  it("confirms the current local edit without rewriting live input", () => {
    const tracker = createComposerDraftVersionTracker("session:one");
    recordComposerLocalDraftEdit(tracker, "A");
    recordComposerLocalDraftEdit(tracker, "AB");

    expect(
      shouldApplyComposerDraftProp({
        prompt: "AB",
        revision: 2,
        scopeKey: "session:one",
        tracker
      })
    ).toBe(false);
    expect(tracker.pendingLocalEchoes).toEqual([]);
  });

  it("applies a true external replacement after a scope change", () => {
    const tracker = createComposerDraftVersionTracker("session:one");
    recordComposerLocalDraftEdit(tracker, "AB");

    expect(
      shouldApplyComposerDraftProp({
        prompt: "from history",
        scopeKey: "session:two",
        tracker
      })
    ).toBe(true);
    expect(tracker.scopeKey).toBe("session:two");
    expect(tracker.nextRevision).toBe(0);
    expect(tracker.pendingLocalEchoes).toEqual([]);
  });

  it("does not resurrect a submitted draft from a lagging echo", () => {
    const tracker = createComposerDraftVersionTracker("session:one");
    const revision = recordComposerLocalDraftEdit(tracker, "AB");
    consumeComposerSubmittedRevision(tracker, revision);

    expect(
      shouldApplyComposerDraftProp({
        prompt: "AB",
        revision,
        scopeKey: "session:one",
        tracker
      })
    ).toBe(false);
    expect(
      shouldApplyComposerDraftProp({
        prompt: "",
        scopeKey: "session:one",
        tracker
      })
    ).toBe(true);
  });

  it("keeps a retyped draft after the previous send is cleared", () => {
    const tracker = createComposerDraftVersionTracker("session:one");
    const submitted = recordComposerLocalDraftEdit(tracker, "hello");
    consumeComposerSubmittedRevision(tracker, submitted);
    recordComposerLocalDraftEdit(tracker, "hello");

    expect(
      shouldApplyComposerDraftProp({
        prompt: "",
        scopeKey: "session:one",
        tracker
      })
    ).toBe(false);
    expect(tracker.nextRevision).toBe(2);
  });

  it("applies an unversioned queue restore when it is not a local echo", () => {
    const tracker = createComposerDraftVersionTracker("session:one");
    recordComposerLocalDraftEdit(tracker, "AB");

    expect(
      shouldApplyComposerDraftProp({
        prompt: "queued prompt",
        scopeKey: "session:one",
        tracker
      })
    ).toBe(true);
    expect(tracker.pendingLocalEchoes).toEqual([]);
    expect(tracker.nextRevision).toBe(2);
  });
});
