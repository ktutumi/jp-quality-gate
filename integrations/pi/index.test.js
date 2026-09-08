import test from "node:test";
import assert from "node:assert/strict";

import {
  correctionContext,
  correctionDeliveryOptions,
  extractAssistantText,
  filterCorrectionMessages,
  isTerminalAssistantTurn,
  validateGateResult,
} from "./index.js";

const validPass = {
  pass: true,
  summary: { errors: 0, warnings: 0, issues: 0 },
  issues: [],
};

const validFail = {
  pass: false,
  summary: { errors: 1, warnings: 0, issues: 1 },
  issues: [
    {
      rule: "simplified_chinese_form",
      severity: "error",
      message: "Suspicious simplified Chinese form",
      text: "经",
      line: 1,
      column: 4,
      details: { japanese_candidates: ["経"] },
    },
  ],
};

test("extractAssistantText joins only text blocks", () => {
  const text = extractAssistantText({
    role: "assistant",
    content: [
      { type: "thinking", thinking: "hidden" },
      { type: "text", text: "first" },
      { type: "toolCall", name: "read" },
      { type: "text", text: "second" },
    ],
  });
  assert.equal(text, "first\nsecond");
});

test("isTerminalAssistantTurn accepts a text-only final turn", () => {
  assert.equal(
    isTerminalAssistantTurn({
      message: { role: "assistant", content: [{ type: "text", text: "完了しました。" }] },
      toolResults: [],
    }),
    true,
  );
});

test("isTerminalAssistantTurn skips tool-call turns and failed turns", () => {
  assert.equal(
    isTerminalAssistantTurn({
      message: {
        role: "assistant",
        content: [{ type: "toolCall", name: "read" }, { type: "text", text: "確認します" }],
      },
      toolResults: [],
    }),
    false,
  );
  assert.equal(
    isTerminalAssistantTurn({
      message: { role: "assistant", stopReason: "error", content: [{ type: "text", text: "x" }] },
      toolResults: [],
    }),
    false,
  );
});

test("validateGateResult accepts current gate schema", () => {
  assert.equal(validateGateResult(validPass), undefined);
  assert.equal(validateGateResult(validFail), undefined);
});

test("validateGateResult rejects malformed or inconsistent output", () => {
  assert.match(validateGateResult({}), /pass/);
  assert.match(
    validateGateResult({ ...validPass, summary: { errors: 0, warnings: 0, issues: 1 } }),
    /issues/,
  );
  assert.match(validateGateResult({ ...validFail, pass: true }), /inconsistent/);
});

test("correctionContext includes compact diagnostics and candidate", () => {
  const text = correctionContext(validFail, 1, 2);
  assert.match(text, /修正試行 1\/2/);
  assert.match(text, /simplified_chinese_form/);
  assert.match(text, /候補|candidates/);
  assert.match(text, /経/);
});

test("correction delivery uses steering so it runs before queued follow-ups", () => {
  assert.deepEqual(correctionDeliveryOptions(), { deliverAs: "steer" });
});

test("filterCorrectionMessages hides historical diagnostics but keeps the active correction", () => {
  const messages = [
    { role: "user", content: "start" },
    { role: "custom", customType: "jp-quality-gate-correction", content: "old" },
    { role: "assistant", content: [{ type: "text", text: "draft" }] },
    { role: "custom", customType: "jp-quality-gate-correction", content: "latest" },
  ];

  assert.deepEqual(
    filterCorrectionMessages(messages, false).map((m) => m.content),
    ["start", [{ type: "text", text: "draft" }]],
  );
  assert.deepEqual(
    filterCorrectionMessages(messages, true).map((m) => m.content),
    ["start", [{ type: "text", text: "draft" }], "latest"],
  );
});

test("prose findings steer a correction, re-check passes, tool failure is fail-open", async () => {
  const { proseFixture } = await import("../prose-test-helper.js");
  const { default: extension } = await import("./index.js");
  const fixture = proseFixture();
  try {
    const handlers = {};
    const messages = [];
    const notifications = [];
    extension({ on: (name, fn) => { handlers[name] = fn; }, sendMessage: (...args) => messages.push(args) });
    const ctx = { ui: { notify: (...args) => notifications.push(args) } };
    const event = (text) => ({ message: { role: "assistant", content: text }, toolResults: [] });
    await handlers.agent_start();
    await handlers.turn_end(event("包括的な説明です。"), ctx);
    assert.equal(messages.length, 1);
    assert.deepEqual(messages[0][1], { deliverAs: "steer" });
    assert.match(messages[0][0].content, /textlint:style/);
    assert.match(messages[0][0].content, /natural-japanese:translationese/);
    assert.match(messages[0][0].content, /source: textlint/);
    assert.match(messages[0][0].content, /数値は変更しない/);
    await handlers.turn_end(event("使い方を説明します。"), ctx);
    assert.equal(messages.length, 1);
    process.env.JPQG_TEXTLINT_BIN = "/missing-jpqg-textlint";
    await handlers.turn_end(event("包括的な説明です。"), ctx);
    assert.equal(messages.length, 1);
    assert.equal(notifications.at(-1)[1], "error");
  } finally { fixture.cleanup(); }
});
