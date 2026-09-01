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
