import assert from "node:assert/strict";
import { randomBytes, createHash } from "node:crypto";
import { readFile, readdir, writeFile } from "node:fs/promises";
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
const measurementScriptSHA256 = createHash("sha256").update(await readFile(fileURLToPath(import.meta.url))).digest("hex");
const variant = process.env.JPQG_CJ_VARIANT ?? (remote ? "unverified" : "packed");
assert.ok(["legacy", "packed", ...(remote ? ["unverified"] : [])].includes(variant));
const firstCase = process.env.JPQG_MEASURE_FIRST ?? "1024";
assert.ok(["1024", "10240", "102400", "262144", "escaped", "concurrent"].includes(firstCase));
let buildManifest;
const profileEnabled = process.env.JPQG_MEASURE_PROFILE === "1";
assert.ok(!profileEnabled || !remote, "profiling is local-only");
let profileSocket;
let profileCall;
let profilePath;
async function startProfile() {
  const inspector = await worker.getInspectorURL();
  inspector.protocol = inspector.protocol === "wss:" ? "https:" : "http:";
  const targets = await (await fetch(new URL("/json/list", inspector), { signal: AbortSignal.timeout(10000) })).json();
  const target = targets.find((t) => t.webSocketDebuggerUrl && t.title.includes("jpqg-measurement")) ?? targets.find((t) => t.webSocketDebuggerUrl);
  assert.ok(target, "inspector target missing");
  profileSocket = new WebSocket(target.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("inspector connection timeout")), 10000);
    profileSocket.onopen = () => { clearTimeout(timer); resolve(); };
    profileSocket.onerror = () => { clearTimeout(timer); reject(new Error("inspector connection failed")); };
  });
  let id = 0;
  const pending = new Map();
  profileSocket.onmessage = ({ data }) => {
    const message = JSON.parse(data);
    const request = pending.get(message.id);
    if (request) { pending.delete(message.id); message.error ? request.reject(new Error(message.error.message)) : request.resolve(message.result); }
  };
  profileCall = (method) => new Promise((resolve, reject) => {
    const callID = ++id;
    const timer = setTimeout(() => { pending.delete(callID); reject(new Error(`profiler timeout: ${method}`)); }, 10000);
    pending.set(callID, { resolve: (v) => { clearTimeout(timer); resolve(v); }, reject: (e) => { clearTimeout(timer); reject(e); } });
    profileSocket.send(JSON.stringify({ id: callID, method }));
  });
  await profileCall("Profiler.enable");
  await profileCall("Profiler.start");
}

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
  const timing = { request_start: 0, response_headers_received: null, response_body_complete: null, json_parse_complete: null };
  const requestStartedAt = new Date().toISOString();
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
    timing.response_headers_received = performance.now() - started;
    const body = await response.text();
    timing.response_body_complete = performance.now() - started;
    result = JSON.parse(body);
    timing.json_parse_complete = performance.now() - started;
  } catch (error) {
    records.push({ label, request_started_at: requestStartedAt, client_timing_ms: timing, measurement_id: measurementID, concurrency,
      input_bytes: Buffer.byteLength(text), status: response?.status ?? null,
      client_elapsed_ms: performance.now() - started, cpu_ms: null, worker_wall_ms: null,
      transport_error: error instanceof Error ? error.name : "Error" });
    throw new Error(`measurement failed: ${label} (${error instanceof Error ? error.name : "Error"})`);
  }
  const elapsed = performance.now() - started;
  const meta = result?.meta ?? {};
  records.push({ label, request_started_at: requestStartedAt, client_timing_ms: timing, measurement_id: measurementID, concurrency, input_bytes: Buffer.byteLength(text), status: response.status,
    client_elapsed_ms: elapsed, cpu_ms: null, worker_wall_ms: null,
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
  records.at(-1).client_elapsed_ms = performance.now() - started;
  return result;
}

try {
  if (remote) {
    endpoint = new URL(remote);
    if (process.env.JPQG_MEASURE_BUILD_MANIFEST) {
      buildManifest = JSON.parse(await readFile(process.env.JPQG_MEASURE_BUILD_MANIFEST, "utf8"));
      assert.ok(["legacy", "packed"].includes(buildManifest.variant));
      if (variant !== "unverified") assert.equal(buildManifest.variant, variant);
    }
  } else {
    const dist = new URL(`dist/${variant}/`, root);
    buildManifest = JSON.parse(await readFile(new URL("build-manifest.json", dist), "utf8"));
    const wasmName = (await readdir(dist)).find((name) => name.endsWith(".wasm"));
    assert.ok(wasmName, "run npm run build first");
    const source = await readFile(new URL("index.js", dist), "utf8");
    const wasm = await readFile(new URL(wasmName, dist));
    bundleSHA256 = createHash("sha256").update(source).update(wasm).digest("hex");
    const config = await readFile(new URL("wrangler.validation.jsonc", root), "utf8");
    const compatibilityDate = config.match(/"compatibility_date"\s*:\s*"([^"]+)"/)[1];
    worker = new Miniflare({ ...(profileEnabled ? { inspectorPort: 0 } : {}), workers: [{ config: {
      type: "worker", name: "jpqg-measurement", compatibilityDate,
      manifest: { mainModule: "index.js", modulesRoot: fileURLToPath(dist), modules: {
        "index.js": { type: "esm", contents: source },
        [wasmName]: { type: "wasm", contents: new Uint8Array(wasm) },
      } },
      env: { JPQG_API_TOKEN: { type: "text", value: token } }, exports: {},
    } }] });
    endpoint = new URL("/v1/check", await worker.ready);
  }
  if (profileEnabled) await startProfile();
  if (firstCase === "escaped") {
    await measure("first-escaped-256KiB", "a".repeat(262144), 1, 200, '{"text":"' + "\\u0061".repeat(262144) + '"}');
  } else if (firstCase === "concurrent") {
    const coldRequests = await Promise.allSettled(Array.from({ length: 4 }, (_, i) =>
      measure(`first-concurrent-${i}`, textOfSize(262144, "a".repeat(i) + "经"), 4)));
    for (let i = 0; i < coldRequests.length; i++) {
      assert.equal(coldRequests[i].status, "fulfilled");
      assert.equal(coldRequests[i].value.issues.find((issue) => issue.rule === "simplified_chinese_form")?.start, i);
    }
  } else {
    await measure(`first-${firstCase}`, textOfSize(Number(firstCase)));
  }
  if (profileEnabled) {
    const { profile } = await profileCall("Profiler.stop");
    profilePath = `.generated/${variant}/cold-${firstCase}.cpuprofile`;
    await writeFile(new URL(profilePath, root), JSON.stringify(profile));
    profileSocket.close();
  }
  for (const bytes of [1024, 10240, 102400, 262144]) {
    await measure(`reuse-${bytes}`, textOfSize(bytes));
  }
  await measure("empty", "");
  for (const [name, clause] of [["kana-free", "経済政策検討"], ["many-issues", "经"], ["markdown", "```js\nconst x = 1;\n```\n説明😀𠮷。\n"]]) {
    const text = clause.repeat(Math.floor(262144 / Buffer.byteLength(clause)));
    await measure(name, text);
  }
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
    if (firstCase !== "concurrent") assert.ok(successful.slice(1).every((record) => record.initialization === "warm"));
    assert.equal(new Set(successful.map((record) => record.isolate_id)).size, 1);
  }
  completed = true;
} finally {
  console.log(JSON.stringify({ completed, measurement_script_sha256: measurementScriptSHA256, profile_path: profilePath ?? null, profile_enabled: profileEnabled, variant, first_case: firstCase, build_manifest: buildManifest ?? null, measured_at: new Date().toISOString(),
    scope: remote ? "deployed HTTP" : "local Miniflare/workerd HTTP",
    endpoint: endpoint ? `${endpoint.origin}${endpoint.pathname}` : null,
    expected_deployed_version_id: remote ? process.env.JPQG_DEPLOYED_VERSION ?? null : null,
    deployment_provenance: remote ? "operator-supplied manifest/version expectation; verify via measurement-ID live tail correlation" : null,
    bundle_sha256: bundleSHA256 ?? null,
    peak_isolate_memory_bytes: null,
    method: "client wall time; response isolate identity and initialization state; sampled Wasm capacity (not total isolate peak); CPU requires separate profiler or correlated live tail",
    records }, null, 2));
  profileSocket?.close();
  await worker?.dispose();
}
