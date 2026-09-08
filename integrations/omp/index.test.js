import { describe, expect, test } from "bun:test";
import { validateGateResult } from "./index.js";

describe("validateGateResult", () => {
  test("accepts a valid passing result", () => {
    expect(
      validateGateResult({
        pass: true,
        summary: { errors: 0, warnings: 0, issues: 0 },
        issues: [],
        meta: {},
      }),
    ).toBeNull();
  });

  test("accepts a valid failing result", () => {
    expect(
      validateGateResult({
        pass: false,
        summary: { errors: 1, warnings: 0, issues: 1 },
        issues: [
          {
            rule: "simplified_chinese_form",
            severity: "error",
            text: "经",
          },
        ],
        meta: {},
      }),
    ).toBeNull();
  });

  test("rejects syntactically valid but malformed JSON", () => {
    expect(validateGateResult({})).toBe("missing boolean pass");
  });

  test("rejects mismatched summary counts", () => {
    expect(
      validateGateResult({
        pass: false,
        summary: { errors: 1, warnings: 0, issues: 0 },
        issues: [],
      }),
    ).toContain("summary.errors");
  });

  test("rejects pass values inconsistent with errors", () => {
    expect(
      validateGateResult({
        pass: true,
        summary: { errors: 1, warnings: 0, issues: 1 },
        issues: [{ rule: "chinese_segment", severity: "error" }],
      }),
    ).toContain("pass (true) is inconsistent");
  });
});

test("prose findings continue a correction, re-check passes, tool failure is fail-open", async () => {
  const { proseFixture } = await import("../prose-test-helper.js");
  const { default: extension } = await import("./index.js");
  const fixture = proseFixture();
  try {
    const handlers = {};
    const notifications = [];
    extension({ on: (name, fn) => { handlers[name] = fn; } });
    const ctx = { ui: { notify: (...args) => notifications.push(args) } };
    const event = (text) => ({ session_id: "test", turn_id: "turn", signal: new AbortController().signal, last_assistant_message: { content: text } });
    const result = await handlers.session_stop(event("包括的な説明です。"), ctx);
    expect(result.continue).toBe(true);
    expect(result.additionalContext).toContain("textlint:style");
    expect(result.additionalContext).toContain("natural-japanese:translationese");
    expect(result.additionalContext).toContain("source: textlint");
    expect(result.additionalContext).toContain("数値は変更しない");
    expect(await handlers.session_stop(event("使い方を説明します。"), ctx)).toBeUndefined();
    process.env.JPQG_TEXTLINT_BIN = "/missing-jpqg-textlint";
    expect(await handlers.session_stop(event("包括的な説明です。"), ctx)).toBeUndefined();
    expect(notifications.at(-1)[1]).toBe("error");
  } finally { fixture.cleanup(); }
}, 60000);
