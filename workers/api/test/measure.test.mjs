import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

test("measurement preserves a failed request without exposing credentials", () => {
  const token = "measurement-test-secret";
  const result = spawnSync(process.execPath, ["scripts/measure.mjs"], {
    cwd: fileURLToPath(new URL("../", import.meta.url)),
    env: { ...process.env, JPQG_MEASURE_URL: "https://127.0.0.1:0/v1/check", JPQG_API_TOKEN: token },
    encoding: "utf8", timeout: 15_000,
  });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 1);
  const report = JSON.parse(result.stdout);
  assert.equal(report.completed, false);
  assert.equal(report.records.length, 1);
  assert.equal(report.records[0].label, "first-1KiB");
  assert.equal(report.records[0].status, null);
  assert.equal(typeof report.records[0].transport_error, "string");
  assert.ok(report.records[0].measurement_id);
  assert.ok(!result.stdout.includes(token));
  assert.ok(!result.stderr.includes(token));
});
