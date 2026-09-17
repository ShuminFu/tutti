import assert from "node:assert/strict";
import test from "node:test";
import {
  contextWindowTokensFromModelUsage,
  readSDKAssistantUsage
} from "./sdkMessages.ts";
import type { SDKMessage } from "@anthropic-ai/claude-agent-sdk";

test("model usage context window follows the active model regardless of map order", () => {
  const modelUsages = [
    {
      "claude-haiku-4-5": { contextWindow: 200_000 },
      "claude-opus-4-6": { contextWindow: 1_000_000 }
    },
    {
      "claude-opus-4-6": { contextWindow: 1_000_000 },
      "claude-haiku-4-5": { contextWindow: 200_000 }
    }
  ];

  for (const modelUsage of modelUsages) {
    assert.equal(
      contextWindowTokensFromModelUsage(modelUsage, "opus"),
      1_000_000
    );
  }
});

test("assistant usage prefers the nested transcript message object", () => {
  const usage = readSDKAssistantUsage({
    type: "assistant",
    message: {
      usage: {
        input_tokens: 100,
        output_tokens: 20
      }
    }
  } as SDKMessage);
  assert.deepEqual(usage, { input_tokens: 100, output_tokens: 20 });
});

test("model usage does not borrow another model context window", () => {
  assert.equal(
    contextWindowTokensFromModelUsage(
      {
        "claude-haiku-4-5": { contextWindow: 200_000 },
        "claude-sonnet-4-6": { contextWindow: 1_000_000 }
      },
      "opus"
    ),
    0
  );
});
