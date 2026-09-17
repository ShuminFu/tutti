import { afterEach, describe, expect, it } from "vitest";
import { setAgentGuiI18nTestLocale } from "../../../i18n/testUtils";
import {
  AGENT_SESSION_TITLE_TOO_LONG_REASON,
  formatPromptSendFailed,
  getAgentGUIErrorMessage
} from "./agentGuiController.errors";

describe("getAgentGUIErrorMessage", () => {
  afterEach(() => setAgentGuiI18nTestLocale("en"));

  it("localizes unavailable configuration dependencies", () => {
    setAgentGuiI18nTestLocale("zh-CN");

    expect(
      getAgentGUIErrorMessage({
        reason: "agent.config_dependency_unavailable",
        params: {
          provider: "codex",
          configKey: "model_instructions_file",
          dependencyPath: "instructions.md",
          failureKind: "missing"
        }
      })
    ).toBe("Codex 的配置引用了当前不可用的文件，请检查本机配置后重试");
  });

  it("localizes the structured session title limit error", () => {
    setAgentGuiI18nTestLocale("zh-CN");

    expect(
      getAgentGUIErrorMessage({
        debugMessage:
          "invalid agent session request: title must be at most 120 characters",
        params: { maxCharacters: 120 },
        reason: AGENT_SESSION_TITLE_TOO_LONG_REASON
      })
    ).toBe("会话标题不能超过 120 个字符。");
  });

  it("uses a localized fallback when the limit param is absent", () => {
    setAgentGuiI18nTestLocale("zh-CN");

    expect(
      getAgentGUIErrorMessage({
        debugMessage:
          "invalid agent session request: title must be at most 120 characters",
        reason: AGENT_SESSION_TITLE_TOO_LONG_REASON
      })
    ).toBe("会话标题过长。");
  });
});

describe("formatPromptSendFailed", () => {
  afterEach(() => setAgentGuiI18nTestLocale("en"));

  it("maps prompt image unsupported failures", () => {
    expect(
      formatPromptSendFailed({
        errorCode: "agent.prompt_image_unsupported",
        errorMessage: "agent prompt image input is unsupported"
      })
    ).toBe("This agent does not support image input with the current model.");
  });

  it("maps the production invalid_request + reason envelope", () => {
    expect(
      formatPromptSendFailed({
        errorCode: "invalid_request",
        errorReason: "agent.prompt_image_unsupported",
        errorMessage: "This agent does not support image input yet."
      })
    ).toBe("This agent does not support image input with the current model.");
  });

  it("maps permission mode mismatches", () => {
    setAgentGuiI18nTestLocale("zh-CN");
    expect(
      formatPromptSendFailed({
        errorCode: "invalid_request",
        errorReason: "agent.permission_mode_unavailable",
        errorMessage: "Mode dontAsk is not available in this session"
      })
    ).toBe("当前会话不支持这个权限档位。请改选其他权限后再发送。");
  });

  it("keeps a generic send failure visible", () => {
    expect(formatPromptSendFailed({ errorMessage: "transport reset" })).toBe(
      "The message could not be sent. transport reset"
    );
  });
});
