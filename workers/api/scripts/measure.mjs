import assert from "node:assert/strict";
import { randomBytes, createHash } from "node:crypto";
import { readFile, readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { Miniflare } from "miniflare";

// Only synthetic inputs and selected diagnostics are printed; never headers or bodies.
const root = new URL("../", import.meta.url);
const remote = process.env.JPQG_MEASURE_URL;
const token = remote ? process.env.JPQG_API_TOKEN : randomBytes(32).toString("hex");
assert.ok(token, "JPQG_API_TOKEN is required for a remote measurement");
if (remote) assert.equal(new URL(remote).protocol, "https:");
let worker;
let endpoint;
let bundleSHA256;
const records = [];
let completed = false;

function textOfSize(bytes, prefix = "") {
  const clause = "これは経済に関する説明です。\n";
  const available = bytes - Buffer.byteLength(prefix);
  const count = Math.floor(available / Buffer.byteLength(clause));
  return prefix + clause.repeat(count) + "a".repeat(available - count * Buffer.byteLength(clause));
}

async function measure(label, text, concurrency = 1, status = 200, raw) {
  const measurementID = `${randomBytes(12).toString("hex")}-${label}`;
  const started = performance.now();
  let response;
  let result;
  try {
    response = await fetch(endpoint, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${token}`,
        "x-jpqg-measurement": measurementID },
      body: raw ?? JSON.stringify({ text }),
      ...(raw instanceof ReadableStream ? { duplex: "half" } : {}),
      redirect: "error",
      signal: AbortSignal.timeout(30_000),
    });
    result = await response.json();
  } catch (error) {
    records.push({ label, measurement_id: measurementID, concurrency,
      input_bytes: Buffer.byteLength(text), status: response?.status ?? null,
      client_elapsed_ms: performance.now() - started, cpu_ms: null,
      transport_error: error instanceof Error ? error.name : "Error" });
    throw new Error(`measurement failed: ${label}`, { cause: error });
  }
  const elapsed = performance.now() - started;
  const meta = result?.meta ?? {};
  records.push({ label, measurement_id: measurementID, concurrency, input_bytes: Buffer.byteLength(text), status: response.status,
    client_elapsed_ms: elapsed, cpu_ms: null,
    isolate_id: meta.validation_isolate_id ?? null,
    sequence: meta.validation_request_sequence ?? null,
    initialization: meta.validation_initialization ?? null,
    wasm_linear_memory_bytes: meta.validation_wasm_memory_bytes ?? null,
    summary: result?.summary ?? null });
  assert.equal(response.status, status, label);
  if (status === 200) {
    assert.equal(typeof result.pass, "boolean");
    assert.ok(Array.isArray(result.issues));
    assert.equal(typeof meta.validation_isolate_id, "string", "deploy the instrumented build first");
    assert.ok(["cold", "waiting", "warm"].includes(meta.validation_initialization));
  }
  return result;
}

try {
  if (remote) {
    endpoint = new URL(remote);
  } else {
    const dist = new URL("dist/", root);
    const wasmName = (await readdir(dist)).find((name) => name.endsWith(".wasm"));
    assert.ok(wasmName, "run npm run build first");
    const source = await readFile(new URL("index.js", dist), "utf8");
    const wasm = await readFile(new URL(wasmName, dist));
    bundleSHA256 = createHash("sha256").update(source).update(wasm).digest("hex");
    const config = await readFile(new URL("wrangler.validation.jsonc", root), "utf8");
    const compatibilityDate = config.match(/"compatibility_date"\s*:\s*"([^"]+)"/)[1];
    worker = new Miniflare({ workers: [{ config: {
      type: "worker", name: "jpqg-measurement", compatibilityDate,
      manifest: { mainModule: "index.js", modulesRoot: fileURLToPath(dist), modules: {
        "index.js": { type: "esm", contents: source },
        [wasmName]: { type: "wasm", contents: new Uint8Array(wasm) },
      } },
      env: { JPQG_API_TOKEN: { type: "text", value: token } }, exports: {},
    } }] });
    endpoint = new URL("/v1/check", await worker.ready);
  }
  await measure("first-1KiB", textOfSize(1024));
  for (const bytes of [1024, 10240, 102400, 262144]) {
    await measure(`reuse-${bytes}`, textOfSize(bytes));
  }
  await measure("empty", "");
  await measure("decoded-overflow", textOfSize(262145), 1, 413);
  await measure("escaped-256KiB", "a".repeat(262144), 1, 200,
    '{"text":"' + "\\u0061".repeat(262144) + '"}');
  const rawOverflow = new ReadableStream({ start(controller) {
    controller.enqueue(new TextEncoder().encode('{"text":""}'));
    for (let i = 0; i < 25; i++) controller.enqueue(new Uint8Array(65536).fill(32));
    controller.close();
  } });
  await measure("raw-overflow", "", 1, 413, rawOverflow);
  const concurrent = await Promise.allSettled(Array.from({ length: 4 }, (_, i) =>
    measure(`concurrent-${i}`, textOfSize(262144, "a".repeat(i) + "经"), 4)));
  for (let i = 0; i < concurrent.length; i++) {
    assert.equal(concurrent[i].status, "fulfilled", `concurrent-${i} failed`);
    assert.equal(concurrent[i].value.issues.find((issue) => issue.rule === "simplified_chinese_form")?.start, i);
  }
  if (!remote) {
    const successful = records.filter((record) => record.status === 200);
    assert.equal(successful[0].initialization, "cold");
    assert.ok(successful.slice(1).every((record) => record.initialization === "warm"));
    assert.equal(new Set(successful.map((record) => record.isolate_id)).size, 1);
  }
  completed = true;
} finally {
  console.log(JSON.stringify({ completed, measured_at: new Date().toISOString(),
    scope: remote ? "deployed HTTP" : "local Miniflare/workerd HTTP", bundle_sha256: bundleSHA256 ?? null,
    peak_isolate_memory_bytes: null,
    method: "client wall time; response isolate identity and initialization state; sampled Wasm capacity (not total isolate peak); CPU requires separate profiler or correlated live tail",
    records }, null, 2));
  await worker?.dispose();
}
