import { describe, expect, it } from "vitest";
import {
  acknowledgeAgentGUIComposerDefaultsMutation,
  createAgentGUIComposerDefaultsLedger,
  prepareAcknowledgedComposerDefaultsAuthorityRead,
  preserveAcknowledgedComposerDefaultsForReconciliation,
  registerAgentGUIComposerDefaultsMutation,
  removeRetiredComposerDefaults,
  retireAcknowledgedComposerDefaultsForRead,
  rollbackRejectedComposerDefaults
} from "./agentGuiComposerDefaultsReconciliation";

const draftKey = "__agent_gui_node_defaults__:target:local:opencode";

describe("agentGuiComposerDefaultsReconciliation", () => {
  it("does not let an older A read retire a later A generation", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const firstA = registerAgentGUIComposerDefaultsMutation(ledger, draftKey, {
      permissionModeId: "ask"
    });
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, firstA, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });
    const firstRead = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      { permissionModeId: "ask" }
    );
    expect(firstRead.settings).toEqual({});
    expect(firstRead.receipt).not.toBeNull();

    const mutationB = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { permissionModeId: "full-access" }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutationB, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });
    const latestA = registerAgentGUIComposerDefaultsMutation(ledger, draftKey, {
      permissionModeId: "ask"
    });
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, latestA, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });

    expect(
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        firstRead.receipt!,
        {
          permissionModeId: "ask"
        },
        {
          permissionModeId: "ask"
        }
      )
    ).toEqual([]);
    const latestRead = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      { permissionModeId: "ask" }
    );
    expect(latestRead.receipt).not.toBeNull();
    expect(
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        latestRead.receipt!,
        {
          permissionModeId: "ask"
        },
        {
          permissionModeId: "ask"
        }
      )
    ).toEqual([{ field: "permissionModeId", value: "ask" }]);
  });

  it("does not let a read started before ack retire that later ack", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { model: "opencode/model-a" }
    );
    const preAckRead = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      { model: "opencode/model-a" }
    );
    expect(preAckRead).toEqual({
      force: false,
      receipt: null,
      settings: { model: "opencode/model-a" }
    });

    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutation, {
      acknowledgedFields: ["model"],
      rejectedFields: [],
      supersededFields: []
    });
    const postAckRead = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      { model: "opencode/model-a" }
    );
    expect(postAckRead).toMatchObject({
      force: true,
      receipt: {
        draftKey,
        fields: { model: { value: "opencode/model-a" } }
      },
      settings: {}
    });
  });

  it("keeps authority receipts isolated by target draft key", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const otherDraftKey =
      "__agent_gui_node_defaults__:target:local:claude-code";
    const opencodeMutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { speed: "fast" }
    );
    const claudeMutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      otherDraftKey,
      { speed: "normal" }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, opencodeMutation, {
      acknowledgedFields: ["speed"],
      rejectedFields: [],
      supersededFields: []
    });
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, claudeMutation, {
      acknowledgedFields: ["speed"],
      rejectedFields: [],
      supersededFields: []
    });
    const opencodeRead = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      { speed: "fast" }
    );

    expect(
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        opencodeRead.receipt!,
        {
          speed: "fast"
        },
        {
          speed: "fast"
        }
      )
    ).toEqual([{ field: "speed", value: "fast" }]);
    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(ledger, otherDraftKey, {
        speed: "normal"
      }).receipt
    ).not.toBeNull();
  });

  it("keeps optimistic defaults when an authority read returns an older value", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { permissionModeId: "accept_edits" }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutation, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });
    const read = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      { permissionModeId: "accept_edits" }
    );

    expect(
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        read.receipt!,
        { permissionModeId: "accept_edits" },
        { permissionModeId: "default" }
      )
    ).toEqual([]);
    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(ledger, draftKey, {
        permissionModeId: "accept_edits"
      }).receipt
    ).not.toBeNull();
  });

  it("stops forcing reads after bounded concrete authority conflicts", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { permissionModeId: "accept_edits" }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutation, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });
    const settings = { permissionModeId: "accept_edits" };

    for (let attempt = 0; attempt < 2; attempt += 1) {
      const read = prepareAcknowledgedComposerDefaultsAuthorityRead(
        ledger,
        draftKey,
        settings
      );
      expect(read.force).toBe(true);
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        read.receipt!,
        settings,
        { permissionModeId: "default" }
      );
    }

    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(
        ledger,
        draftKey,
        settings
      )
    ).toEqual({
      force: false,
      receipt: null,
      settings
    });
  });

  it("protects acknowledged fields from sanitize only while confirmation remains bounded", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const settings = {
      model: "opencode/model-b",
      permissionModeId: "accept_edits",
      reasoningEffort: "high",
      speed: "fast"
    };
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      settings
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutation, {
      acknowledgedFields: [
        "model",
        "permissionModeId",
        "reasoningEffort",
        "speed"
      ],
      rejectedFields: [],
      supersededFields: []
    });
    const sanitized = {
      model: null,
      permissionModeId: null,
      reasoningEffort: null,
      speed: null
    };

    expect(
      preserveAcknowledgedComposerDefaultsForReconciliation(
        ledger,
        draftKey,
        settings,
        sanitized
      )
    ).toEqual(settings);

    for (let attempt = 0; attempt < 2; attempt += 1) {
      const read = prepareAcknowledgedComposerDefaultsAuthorityRead(
        ledger,
        draftKey,
        settings
      );
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        read.receipt!,
        settings,
        {
          model: "opencode/model-a",
          permissionModeId: "default",
          reasoningEffort: "low",
          speed: "normal"
        }
      );
    }

    expect(
      preserveAcknowledgedComposerDefaultsForReconciliation(
        ledger,
        draftKey,
        settings,
        sanitized
      )
    ).toBe(sanitized);
  });

  it("stops forcing reads but keeps optimistic intent when authority omits the field", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { permissionModeId: "accept_edits" }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutation, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });
    const settings = { permissionModeId: "accept_edits" };
    const read = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      settings
    );

    expect(
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        read.receipt!,
        settings,
        {}
      )
    ).toEqual([]);
    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(
        ledger,
        draftKey,
        settings
      )
    ).toEqual({
      force: false,
      receipt: null,
      settings
    });
  });

  it("reconciles exact, absent, and conflicting fields independently", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      {
        model: "opencode/model-a",
        permissionModeId: "accept_edits",
        speed: "fast"
      }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutation, {
      acknowledgedFields: ["model", "permissionModeId", "speed"],
      rejectedFields: [],
      supersededFields: []
    });
    const settings = {
      model: "opencode/model-a",
      permissionModeId: "accept_edits",
      speed: "fast"
    };
    const read = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      settings
    );

    expect(
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        read.receipt!,
        settings,
        {
          model: "opencode/model-a",
          speed: "normal"
        }
      )
    ).toEqual([{ field: "model", value: "opencode/model-a" }]);

    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(ledger, draftKey, {
        permissionModeId: "accept_edits",
        speed: "fast"
      })
    ).toMatchObject({
      force: true,
      receipt: {
        fields: {
          speed: { value: "fast" }
        }
      },
      settings: {
        permissionModeId: "accept_edits"
      }
    });
  });

  it("does not let an omitted older read release a newer generation", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const firstMutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { permissionModeId: "accept_edits" }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, firstMutation, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });
    const firstRead = prepareAcknowledgedComposerDefaultsAuthorityRead(
      ledger,
      draftKey,
      { permissionModeId: "accept_edits" }
    );
    const secondMutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      { permissionModeId: "dont_ask" }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, secondMutation, {
      acknowledgedFields: ["permissionModeId"],
      rejectedFields: [],
      supersededFields: []
    });

    expect(
      retireAcknowledgedComposerDefaultsForRead(
        ledger,
        firstRead.receipt!,
        { permissionModeId: "dont_ask" },
        {}
      )
    ).toEqual([]);
    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(ledger, draftKey, {
        permissionModeId: "dont_ask"
      })
    ).toMatchObject({
      force: true,
      receipt: {
        fields: {
          permissionModeId: { value: "dont_ask" }
        }
      }
    });
  });

  // The daemon validates a defaults patch per field, so one refused field must
  // not discard the siblings that were persisted.
  it("rolls back only the rejected fields and reports each reason code", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      {
        model: "openai/gpt-5",
        permissionModeId: "ask"
      }
    );

    const rolledBack = rollbackRejectedComposerDefaults(ledger, mutation, {
      acknowledgedFields: ["model"],
      rejectedFields: [
        { field: "permissionModeId", reasonCode: "not_configurable" }
      ],
      supersededFields: []
    });

    expect(rolledBack).toEqual([
      {
        field: "permissionModeId",
        reasonCode: "not_configurable",
        value: "ask"
      }
    ]);
    // The rejected field leaves the ledger so no later read resurrects it.
    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(ledger, draftKey, {
        model: "openai/gpt-5",
        permissionModeId: "ask"
      })
    ).toEqual({
      force: false,
      receipt: null,
      settings: { model: "openai/gpt-5", permissionModeId: "ask" }
    });
  });

  // Race protection: the user changed the same field again while the rejected
  // publish was still in flight, so the newer mutation owns the field. The
  // rejection must be reported (returned) but must never roll back the newer
  // value.
  it("does not roll back a field whose generation was superseded by a newer edit", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const rejected = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      {
        permissionModeId: "ask"
      }
    );
    const newer = registerAgentGUIComposerDefaultsMutation(ledger, draftKey, {
      permissionModeId: "full-access"
    });

    expect(
      rollbackRejectedComposerDefaults(ledger, rejected, {
        acknowledgedFields: [],
        rejectedFields: [
          { field: "permissionModeId", reasonCode: "unsupported_value" }
        ],
        supersededFields: []
      })
    ).toEqual([]);

    // The newer mutation still owns the draft: its optimistic value survives
    // and its own settlement decides the outcome.
    expect(
      prepareAcknowledgedComposerDefaultsAuthorityRead(ledger, draftKey, {
        permissionModeId: "full-access"
      })
    ).toEqual({
      force: false,
      receipt: null,
      settings: { permissionModeId: "full-access" }
    });
    expect(
      acknowledgeAgentGUIComposerDefaultsMutation(ledger, newer, {
        acknowledgedFields: ["permissionModeId"],
        rejectedFields: [],
        supersededFields: []
      })
    ).toBe(true);
  });

  // A late settlement for a cycle the ledger has already moved past must be a
  // no-op: it may neither resurrect the stale value nor double-report.
  it("ignores a settlement for an already-superseded cycle", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const stale = registerAgentGUIComposerDefaultsMutation(ledger, draftKey, {
      model: "old/model"
    });
    registerAgentGUIComposerDefaultsMutation(ledger, draftKey, {
      model: "new/model"
    });

    const first = rollbackRejectedComposerDefaults(ledger, stale, {
      acknowledgedFields: [],
      rejectedFields: [{ field: "model", reasonCode: "invalid_value" }],
      supersededFields: []
    });
    const second = rollbackRejectedComposerDefaults(ledger, stale, {
      acknowledgedFields: [],
      rejectedFields: [{ field: "model", reasonCode: "invalid_value" }],
      supersededFields: []
    });

    expect(first).toEqual([]);
    expect(second).toEqual([]);
    expect(ledger.latestByDraftKey[draftKey]?.model).not.toBeUndefined();
  });

  it("drops the rolled-back draft value while keeping acknowledged siblings", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      {
        model: "openai/gpt-5",
        reasoningEffort: "high"
      }
    );
    acknowledgeAgentGUIComposerDefaultsMutation(ledger, mutation, {
      acknowledgedFields: ["model"],
      rejectedFields: [],
      supersededFields: []
    });
    const rolledBack = rollbackRejectedComposerDefaults(ledger, mutation, {
      acknowledgedFields: ["model"],
      rejectedFields: [
        { field: "reasoningEffort", reasonCode: "unsupported_value" }
      ],
      supersededFields: []
    });

    expect(
      removeRetiredComposerDefaults(
        { model: "openai/gpt-5", reasoningEffort: "high" },
        rolledBack
      )
    ).toEqual({ model: "openai/gpt-5" });
  });

  // A host that has not adopted per-field settlement omits the key entirely;
  // reconciliation must treat that as "nothing rejected" instead of crashing.
  it("tolerates a settlement result without a rejectedFields list", () => {
    const ledger = createAgentGUIComposerDefaultsLedger();
    const mutation = registerAgentGUIComposerDefaultsMutation(
      ledger,
      draftKey,
      {
        model: "openai/gpt-5"
      }
    );

    expect(
      rollbackRejectedComposerDefaults(ledger, mutation, {
        acknowledgedFields: ["model"],
        supersededFields: []
      } as never)
    ).toEqual([]);
  });
});
