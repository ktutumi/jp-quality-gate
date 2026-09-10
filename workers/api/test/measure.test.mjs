import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

test("measurement preserves a failed request without exposing credentials", () => {
  const token = "measurement-test-secret";
  const env = { ...process.env };
  for (const key of Object.keys(env)) if (key.startsWith("JPQG_")) delete env[key];
  const result = spawnSync(process.execPath, ["scripts/measure.mjs"], {
    cwd: fileURLToPath(new URL("../", import.meta.url)),
    env: { ...env, JPQG_MEASURE_URL: "https://127.0.0.1:0/v1/check", JPQG_API_TOKEN: token },
    encoding: "utf8", timeout: 15_000,
  });
  assert.equal(result.error, undefined);
  assert.equal(result.status, 1);
  const report = JSON.parse(result.stdout);
  assert.equal(report.completed, false);
  assert.equal(report.variant, "unverified");
  assert.match(report.measurement_script_sha256, /^[0-9a-f]{64}$/);
  assert.deepEqual(report.records[0].client_timing_ms, { request_start: 0, response_headers_received: null, response_body_complete: null, json_parse_complete: null });
  assert.equal(report.records.length, 1);
  assert.equal(report.records[0].label, "first-1024");
  assert.equal(report.records[0].status, null);
  assert.equal(typeof report.records[0].transport_error, "string");
  assert.ok(report.records[0].measurement_id);
  assert.ok(!result.stdout.includes(token));
  assert.ok(!result.stderr.includes(token));
});
