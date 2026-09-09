import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { after, before, test } from "node:test";

import { Miniflare } from "miniflare";

const testDir = dirname(fileURLToPath(import.meta.url));
const apiRoot = resolve(testDir, "..");
const repoRoot = resolve(apiRoot, "../..");
const distRoot = resolve(apiRoot, "dist");
const workerEntry = resolve(distRoot, "index.js");
const validationConfig = resolve(apiRoot, "wrangler.validation.jsonc");
const testToken = "local-test-token-not-for-production";
const maxTextBytes = 256 * 1024;
const requestTimeoutMs = 30_000;
const testTimeoutMs = 120_000;
const encoder = new TextEncoder();

let compatibilityDate;
let workerBundle;
let worker;
let workerURL;
let cliPath;
let cliTempDir;

before(async () => {
  compatibilityDate = await readCompatibilityDate();
  cliTempDir = await mkdtemp(join(tmpdir(), "jp-quality-gate-http-"));
  cliPath = join(cliTempDir, "jp-quality-gate");
  const build = await runProcess(
    "go",
    ["build", "-o", cliPath, "./cmd/jp-quality-gate"],
    { cwd: repoRoot, env: cleanCLIEnvironment() },
  );
  if (build.code !== 0) {
    throw new Error(`go build failed (${build.code}): ${build.stderr || build.stdout}`);
  }
  workerBundle = loadWorkerBundle();

  worker = createWorker(testToken);
  try {
    workerURL = new URL(await worker.ready);
  } catch (error) {
    await worker.dispose();
    worker = undefined;
    throw error;
  }
}, { timeout: testTimeoutMs });

after(async () => {
  try {
    if (worker) await worker.dispose();
  } finally {
    if (cliTempDir) await rm(cliTempDir, { recursive: true, force: true });
  }
}, { timeout: testTimeoutMs });

function cleanCLIEnvironment() {
  const env = { ...process.env };
  for (const name of Object.keys(env)) {
    if (name.startsWith("JPQG_")) delete env[name];
  }
  delete env.GOOS;
  delete env.GOARCH;
  return env;
}

async function readCompatibilityDate() {
  const source = await readFile(validationConfig, "utf8");
  const match = source.match(/^\s*"compatibility_date"\s*:\s*"([^"]+)"/m);
  assert.ok(match, `compatibility_date missing from ${validationConfig}`);
  return match[1];
}

function loadWorkerBundle() {
  const wasmName = readdirSync(distRoot).find((name) => name.endsWith(".wasm"));
  assert.ok(wasmName, `compiled Wasm asset missing from ${distRoot}`);
  return {
    wasmName,
    workerSource: readFileSync(workerEntry, "utf8"),
    wasmContents: new Uint8Array(readFileSync(resolve(distRoot, wasmName))),
  };
}

function createWorker(token) {
  const config = {
    type: "worker",
    name: "jpqg-validation-test",
    compatibilityDate,
    manifest: {
      mainModule: "index.js",
      modulesRoot: distRoot,
      modules: {
        "index.js": { type: "esm", contents: workerBundle.workerSource },
        [workerBundle.wasmName]: { type: "wasm", contents: workerBundle.wasmContents },
      },
    },
    env: token === undefined ? {} : { JPQG_API_TOKEN: { type: "text", value: token } },
    exports: {},
  };
  return new Miniflare({ workers: [{ config }] });
}

async function withFreshWorker(token, callback) {
  const freshWorker = createWorker(token);
  try {
    const freshURL = new URL(await freshWorker.ready);
    return await callback(freshURL);
  } finally {
    await freshWorker.dispose();
  }
}

function endpoint(baseURL) {
  return new URL("/v1/check", baseURL);
}

async function postJSON(baseURL, payload, options = {}) {
  return postRaw(baseURL, JSON.stringify(payload), options);
}

async function postRaw(baseURL, body, options = {}) {
  const contentType = options.contentType ?? "application/json";
  const authorization = Object.hasOwn(options, "authorization") ? options.authorization : `Bearer ${testToken}`;
  const headers = {};
  if (contentType !== undefined) headers["content-type"] = contentType;
  if (authorization !== undefined && authorization !== null) headers.authorization = authorization;
  const request = {
    method: "POST",
    headers,
    body,
    signal: AbortSignal.timeout(requestTimeoutMs),
  };
  if (typeof ReadableStream !== "undefined" && body instanceof ReadableStream) request.duplex = "half";
  const response = await fetch(endpoint(baseURL), request);
  const text = await response.text();
  let json;
  try {
    json = JSON.parse(text);
  } catch {
    // assertError below reports a useful failure for non-JSON responses.
  }
  return { response, text, json };
}

function assertJSONResponse(response) {
  assert.match(response.response.headers.get("content-type") ?? "", /application\/json/i);
  assert.equal(response.response.headers.get("cache-control"), "no-store");
  assert.ok(response.json && typeof response.json === "object", `response was not JSON: ${response.text}`);
}

function assertError(response, status) {
  assert.equal(response.response.status, status, response.text);
  assertJSONResponse(response);
  assert.deepEqual(Object.keys(response.json).sort(), ["internal_error", "pass"]);
  assert.equal(response.json.pass, false);
  assert.equal(typeof response.json.internal_error, "string");
  assert.doesNotMatch(response.text, new RegExp(escapeRegExp(testToken)));
}

function coreResult(result) {
  const { meta: _meta, ...withoutMeta } = result;
  return withoutMeta;
}

async function assertParity(baseURL, name, text, options = {}) {
  const expected = await runLocalCLI(text, options);
  const actual = await postJSON(baseURL, { text, ...(Object.keys(options).length ? { options } : {}) });
  assert.equal(actual.response.status, 200, `${name}: ${actual.text}`);
  assertJSONResponse(actual);
  assert.deepEqual(coreResult(actual.json), coreResult(expected), `${name}: Worker/local mismatch`);
}

async function runLocalCLI(text, options = {}) {
  const args = [
    "--cj-min-cjk",
    String(options.cj_min_cjk ?? 4),
    "--cj-min-gap",
    String(options.cj_min_gap ?? 0.15),
  ];
  if (options.include_code) args.push("--include-code");
  if (options.warnings_as_errors) args.push("--warnings-as-errors");
  args.push("-");
  const result = await runProcess(cliPath, args, {
    cwd: repoRoot,
    env: cleanCLIEnvironment(),
    input: text,
  });
  assert.ok(result.code === 0 || result.code === 1, `local CLI failed (${result.code}): ${result.stderr || result.stdout}`);
  try {
    return JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`local CLI returned invalid JSON: ${error.message}; stderr=${result.stderr}; stdout=${result.stdout}`);
  }
}

function runProcess(command, args, { cwd, env, input, timeoutMs = testTimeoutMs } = {}) {
  return new Promise((resolveProcess, reject) => {
    const child = spawn(command, args, {
      cwd,
      env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    let stdout = "";
    let stderr = "";
    let settled = false;
    let timedOut = false;
    let killTimer;
    const timeout = setTimeout(() => {
      timedOut = true;
      child.kill("SIGTERM");
      killTimer = setTimeout(() => child.kill("SIGKILL"), 1_000);
    }, timeoutMs);
    const finish = (callback) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      clearTimeout(killTimer);
      callback();
    };
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.once("error", (error) => finish(() => reject(error)));
    child.once("close", (code, signal) => finish(() => resolveProcess({ code, signal, stdout, stderr, timedOut })));
    if (input === undefined) child.stdin.end();
    else child.stdin.end(input);
  });
}

function exactUTF8Text(byteLength) {
  const clause = "これは経済に関する説明です。\n";
  const clauseBytes = encoder.encode(clause).byteLength;
  let text = "";
  let used = 0;
  while (used + clauseBytes <= byteLength) {
    text += clause;
    used += clauseBytes;
  }
  while (used + encoder.encode("あ").byteLength <= byteLength) {
    text += "あ";
    used += encoder.encode("あ").byteLength;
  }
  text += "a".repeat(byteLength - used);
  assert.equal(encoder.encode(text).byteLength, byteLength);
  return text;
}

function unicodeEscapedJSON(text) {
  let escaped = "";
  for (const character of text) {
    for (let index = 0; index < character.length; index += 1) {
      escaped += `\\u${character.charCodeAt(index).toString(16).padStart(4, "0")}`;
    }
  }
  return `{"text":"${escaped}"}`;
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function testWithTimeout(name, callback) {
  return test(name, { timeout: testTimeoutMs }, callback);
}

testWithTimeout("rejects missing, malformed, and incorrect bearer credentials", async () => {
  assertError(await postJSON(workerURL, { text: "日本語" }, { authorization: undefined }), 401);
  assertError(await postJSON(workerURL, { text: "日本語" }, { authorization: "Basic local-test-token-not-for-production" }), 401);
  const wrong = await postJSON(workerURL, { text: "日本語" }, { authorization: "Bearer wrong-token" });
  assertError(wrong, 401);
  assert.doesNotMatch(wrong.text, /wrong-token/);
});

testWithTimeout("rejects requests when the validation secret binding is absent", async () => {
  await withFreshWorker(undefined, async (freshURL) => {
    assertError(await postJSON(freshURL, { text: "日本語" }, { authorization: `Bearer ${testToken}` }), 401);
  });
});

testWithTimeout("rejects malformed JSON, schema, content type, and options", async () => {
  const cases = [
    { name: "invalid JSON", body: "{\"text\":" },
    { name: "invalid UTF-8", body: new Uint8Array([0x7b, 0x22, 0x74, 0x65, 0x78, 0x74, 0x22, 0x3a, 0xff, 0x7d]) },
    { name: "missing text", body: "{}" },
    { name: "non-string text", body: JSON.stringify({ text: 7 }) },
    { name: "null options", body: JSON.stringify({ text: "日本語", options: null }) },
    { name: "unknown top-level field", body: JSON.stringify({ text: "日本語", checks: { unihan: true, cj: true } }) },
    { name: "unknown option", body: JSON.stringify({ text: "日本語", options: { unexpected: true } }) },
    { name: "zero minimum CJK", body: JSON.stringify({ text: "日本語", options: { cj_min_cjk: 0 } }) },
    { name: "out-of-range gap", body: JSON.stringify({ text: "日本語", options: { cj_min_gap: 1.01 } }) },
    { name: "wrong include_code type", body: JSON.stringify({ text: "日本語", options: { include_code: "false" } }) },
    { name: "wrong content type", body: JSON.stringify({ text: "日本語" }), contentType: "text/plain" },
  ];
  for (const item of cases) {
    const response = await postRaw(workerURL, item.body, { contentType: item.contentType ?? "application/json" });
    assertError(response, 400);
  }
});

testWithTimeout("accepts empty input and preserves the complete local result shape", async () => {
  await assertParity(workerURL, "empty input", "");
});

testWithTimeout("matches local Go results for Chinese forms, mixed clauses, Markdown, URLs, code, and astral text", async () => {
  const corpus = [
    { name: "normal Japanese", text: "これは経済に関する説明です。" },
    { name: "simplified Chinese form", text: "これは经済に関する説明です。" },
    { name: "traditional Chinese form", text: "今天天氣很好，我們去公園散步。" },
    { name: "mixed Japanese and Chinese clauses", text: "これは経済に関する説明です。今天天气很好，我们去公园散步。" },
    {
      name: "Markdown code and URL masked",
      text: "本文は経済に関する説明です。\n```text\n这是经済コードです。\n```\n`这是经済`\nhttps://example.com/这是经済\n",
    },
    {
      name: "Markdown code included",
      text: "```text\n这是经済コードです。\n```\n",
      options: { include_code: true },
    },
    { name: "astral before finding", text: "🙂🙂これは经済です。" },
  ];
  for (const item of corpus) {
    await assertParity(workerURL, item.name, item.text, item.options ?? {});
  }
});

testWithTimeout("promotes warnings to errors without changing issue order or messages", async () => {
  const text = "今天天气很好，我们去公园散步。";
  const options = { cj_min_cjk: 4, cj_min_gap: 0.999 };
  const normal = await runLocalCLI(text, options);
  assert.ok(normal.issues.some((issue) => issue.severity === "warning"), "fixture must produce a warning");
  const promoted = await runLocalCLI(text, { ...options, warnings_as_errors: true });
  assert.deepEqual(
    promoted.issues.map(({ rule, message, start, end, text: issueText }) => ({ rule, message, start, end, text: issueText })),
    normal.issues.map(({ rule, message, start, end, text: issueText }) => ({ rule, message, start, end, text: issueText })),
  );
  assert.ok(promoted.issues.every((issue) => issue.severity === "error"));
  await assertParity(workerURL, "warnings retained", text, options);
  await assertParity(workerURL, "warnings promoted", text, { ...options, warnings_as_errors: true });
});

testWithTimeout("accepts exactly 256 KiB UTF-8 text, rejects decoded overflow, and accepts worst-case escapes", async () => {
  const exactText = exactUTF8Text(maxTextBytes);
  const exact = await postJSON(workerURL, { text: exactText });
  assert.equal(exact.response.status, 200, exact.text);
  assertJSONResponse(exact);
  assert.deepEqual(coreResult(exact.json), coreResult(await runLocalCLI(exactText)));

  const overflow = await postJSON(workerURL, { text: `${exactText}a` });
  assertError(overflow, 413);

  const escapedText = "a".repeat(maxTextBytes);
  const escaped = await postRaw(workerURL, unicodeEscapedJSON(escapedText));
  assert.equal(escaped.response.status, 200, escaped.text);
  assertJSONResponse(escaped);
  assert.deepEqual(coreResult(escaped.json), coreResult(await runLocalCLI(escapedText)));

  const envelopePrefix = encoder.encode('{"text":""}');
  const whitespace = new Uint8Array(64 * 1024);
  whitespace.fill(0x20);
  const oversizedEnvelope = new ReadableStream({
    start(controller) {
      controller.enqueue(envelopePrefix);
      for (let index = 0; index < 25; index += 1) controller.enqueue(whitespace);
      controller.close();
    },
  });
  assertError(await postRaw(workerURL, oversizedEnvelope), 413);
});

testWithTimeout("keeps distinct concurrent requests isolated during cold initialization", async () => {
  await withFreshWorker(testToken, async (freshURL) => {
    const requests = [
      { name: "Japanese", text: "これは経済に関する説明です。" },
      { name: "simplified", text: "这是经済。" },
      { name: "traditional", text: "這是經濟。" },
      { name: "astral", text: "🙂🙂これは经済です。", options: { include_code: false } },
    ];
    const [actual, expected] = await Promise.all([
      Promise.all(requests.map((item) => postJSON(freshURL, { text: item.text, ...(item.options ? { options: item.options } : {}) }))),
      Promise.all(requests.map((item) => runLocalCLI(item.text, item.options ?? {}))),
    ]);
    for (let index = 0; index < requests.length; index += 1) {
      assert.equal(actual[index].response.status, 200, `${requests[index].name}: ${actual[index].text}`);
      assertJSONResponse(actual[index]);
      assert.deepEqual(coreResult(actual[index].json), coreResult(expected[index]), `${requests[index].name}: concurrent mismatch`);
    }
    const metas = actual.map(({ json }) => json.meta);
    assert.equal(new Set(metas.map((meta) => meta.validation_isolate_id)).size, 1);
    assert.equal(metas.filter((meta) => meta.validation_initialization === "cold").length, 1);
    assert.deepEqual(metas.map((meta) => meta.validation_request_sequence).sort(), [1, 2, 3, 4]);
    const reused = await postJSON(freshURL, { text: "再利用の確認です。" });
    assert.equal(reused.json.meta.validation_isolate_id, metas[0].validation_isolate_id);
    assert.equal(reused.json.meta.validation_initialization, "warm");
    assert.equal(reused.json.meta.validation_request_sequence, 5);
  });
});
