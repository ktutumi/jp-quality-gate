import "../.generated/wasm_exec.js";
import wasmModule from "../.generated/core.wasm";

type Options = {
  cj_min_cjk: number;
  cj_min_gap: number;
  include_code: boolean;
  warnings_as_errors: boolean;
};

type CheckRequest = {
  text: string;
  options: Options;
};

type JsonRecord = Record<string, unknown>;
type BridgeCheck = (requestJSON: string) => string;
type ReadyCallback = (error: string | null) => void;

type GoRuntime = {
  env: Record<string, string>;
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<unknown>;
};

type BridgeGlobal = typeof globalThis & {
  Go?: new () => GoRuntime;
  __jpqgCheck?: BridgeCheck;
  __jpqgReady?: ReadyCallback;
};

type Core = {
  check: BridgeCheck;
  memory: WebAssembly.Memory;
};

type Env = {
  JPQG_API_TOKEN?: string;
};

const bridgeGlobal = globalThis as BridgeGlobal;
const MAX_TEXT_BYTES = 256 * 1024;
const MAX_BODY_BYTES = MAX_TEXT_BYTES * 6 + 4096;
const INIT_TIMEOUT_MS = 10_000;
const DEFAULT_OPTIONS: Options = {
  cj_min_cjk: 4,
  cj_min_gap: 0.15,
  include_code: false,
  warnings_as_errors: false,
};
const encoder = new TextEncoder();
const BAD_REQUEST = Symbol("bad_request");
const TOO_LARGE = Symbol("too_large");
const INTERNAL_ERROR = Symbol("internal_error");
const ALLOWED_REQUEST_KEYS = ["text", "options"];
const ALLOWED_OPTION_KEYS = [
  "cj_min_cjk",
  "cj_min_gap",
  "include_code",
  "warnings_as_errors",
];

let corePromise: Promise<Core> | undefined;
let isolateID: string | undefined;
let coreReady = false;
let requestSequence = 0;

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasOwn(value: JsonRecord, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function jsonResponse(status: number, body: JsonRecord): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "cache-control": "no-store",
      "content-type": "application/json; charset=utf-8",
    },
  });
}

function errorResponse(status: number, message: string): Response {
  return jsonResponse(status, { pass: false, internal_error: message });
}

async function readBody(request: Request): Promise<Uint8Array> {
  const declaredLength = request.headers.get("content-length");
  if (declaredLength !== null) {
    const length = Number(declaredLength);
    if (Number.isFinite(length) && length > MAX_BODY_BYTES) {
      throw TOO_LARGE;
    }
  }

  if (!request.body) {
    return new Uint8Array();
  }

  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    while (true) {
      const next = await reader.read();
      if (next.done) {
        break;
      }
      length += next.value.byteLength;
      if (length > MAX_BODY_BYTES) {
        try {
          await reader.cancel();
        } catch {
          // The body is already rejected; cancellation failure is not user-visible.
        }
        throw TOO_LARGE;
      }
      chunks.push(next.value);
    }
  } finally {
    reader.releaseLock();
  }

  const body = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return body;
}

function parseRequest(body: Uint8Array): CheckRequest {
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(body);
  } catch {
    throw BAD_REQUEST;
  }

  let decoded: unknown;
  try {
    decoded = JSON.parse(text);
  } catch {
    throw BAD_REQUEST;
  }
  if (!isRecord(decoded) || !Object.keys(decoded).every((key) => ALLOWED_REQUEST_KEYS.includes(key)) || !hasOwn(decoded, "text")) {
    throw BAD_REQUEST;
  }
  if (typeof decoded.text !== "string") {
    throw BAD_REQUEST;
  }
  if (encoder.encode(decoded.text).byteLength > MAX_TEXT_BYTES) {
    throw TOO_LARGE;
  }

  const options: Options = { ...DEFAULT_OPTIONS };
  if (hasOwn(decoded, "options")) {
    if (!isRecord(decoded.options) || !Object.keys(decoded.options).every((key) => ALLOWED_OPTION_KEYS.includes(key))) {
      throw BAD_REQUEST;
    }
    if (hasOwn(decoded.options, "cj_min_cjk")) {
      const value = decoded.options.cj_min_cjk;
      if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 1) {
        throw BAD_REQUEST;
      }
      options.cj_min_cjk = value;
    }
    if (hasOwn(decoded.options, "cj_min_gap")) {
      const value = decoded.options.cj_min_gap;
      if (typeof value !== "number" || !Number.isFinite(value) || value < 0 || value > 1) {
        throw BAD_REQUEST;
      }
      options.cj_min_gap = value;
    }
    if (hasOwn(decoded.options, "include_code")) {
      if (typeof decoded.options.include_code !== "boolean") {
        throw BAD_REQUEST;
      }
      options.include_code = decoded.options.include_code;
    }
    if (hasOwn(decoded.options, "warnings_as_errors")) {
      if (typeof decoded.options.warnings_as_errors !== "boolean") {
        throw BAD_REQUEST;
      }
      options.warnings_as_errors = decoded.options.warnings_as_errors;
    }
  }

  return { text: decoded.text, options };
}

function initializeCore(): Promise<Core> {
  return new Promise<Core>((resolve, reject) => {
    let settled = false;
    const timeout = setTimeout(fail, INIT_TIMEOUT_MS);

    function fail(): void {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timeout);
      reject(INTERNAL_ERROR);
    }

    bridgeGlobal.__jpqgReady = (error) => {
      if (settled) {
        return;
      }
      if (error !== null) {
        fail();
        return;
      }
      const check = bridgeGlobal.__jpqgCheck;
      const go = bridgeGlobal.Go;
      if (typeof check !== "function" || typeof go !== "function") {
        fail();
        return;
      }
      const instanceMemory = currentMemory;
      if (!instanceMemory) {
        fail();
        return;
      }
      settled = true;
      clearTimeout(timeout);
      coreReady = true;
      resolve({ check, memory: instanceMemory });
    };

    let currentMemory: WebAssembly.Memory | undefined;
    try {
      if (typeof bridgeGlobal.Go !== "function") {
        fail();
        return;
      }
      const go = new bridgeGlobal.Go();
      // Go's soft GC target leaves room for JS and Wasm overhead; it is not an isolate limit.
      go.env.GOMEMLIMIT = "48MiB";
      const instance = new WebAssembly.Instance(wasmModule, go.importObject);
      const memory = instance.exports.mem;
      if (!(memory instanceof WebAssembly.Memory)) {
        fail();
        return;
      }
      currentMemory = memory;
      void go.run(instance).then(fail, fail);
    } catch {
      fail();
    }
  });
}

function getCore(): Promise<Core> {
  return (corePromise ??= initializeCore());
}

function withMemoryMeta(rawResult: string, memory: WebAssembly.Memory, diagnostics: JsonRecord): string {
  let result: unknown;
  try {
    result = JSON.parse(rawResult);
  } catch {
    throw INTERNAL_ERROR;
  }
  if (!isRecord(result)) {
    throw INTERNAL_ERROR;
  }
  if (result.pass === false && typeof result.internal_error === "string") {
    throw INTERNAL_ERROR;
  }
  if (!isRecord(result.meta)) {
    throw INTERNAL_ERROR;
  }
  result.meta.validation_wasm_memory_bytes = memory.buffer.byteLength;
  Object.assign(result.meta, diagnostics);
  try {
    return JSON.stringify(result);
  } catch {
    throw INTERNAL_ERROR;
  }
}

function authorized(request: Request, env: Env): boolean {
  const token = env.JPQG_API_TOKEN;
  return typeof token === "string" && token.length > 0 && request.headers.get("authorization") === `Bearer ${token}`;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname !== "/v1/check") {
      return errorResponse(404, "not found");
    }
    if (request.method !== "POST") {
      return errorResponse(405, "method not allowed");
    }
    if (!authorized(request, env)) {
      return errorResponse(401, "unauthorized");
    }
    const contentType = request.headers.get("content-type");
    if (!contentType || !/^application\/json(?:\s*;|\s*$)/i.test(contentType)) {
      return errorResponse(400, "invalid request");
    }

    let parsed: CheckRequest;
    try {
      parsed = parseRequest(await readBody(request));
    } catch (error) {
      if (error === TOO_LARGE) {
        return errorResponse(413, "request too large");
      }
      return errorResponse(400, "invalid request");
    }

    let core: Core;
    // Assigned before awaiting initialization so concurrent waiters are not called warm.
    const diagnostics = {
      validation_isolate_id: (isolateID ??= crypto.randomUUID()),
      validation_request_sequence: ++requestSequence,
      validation_initialization: coreReady ? "warm" : corePromise ? "waiting" : "cold",
    };
    try {
      core = await getCore();
    } catch {
      return errorResponse(503, "service unavailable");
    }

    let rawResult: string;
    try {
      rawResult = core.check(JSON.stringify(parsed));
      if (typeof rawResult !== "string") {
        throw INTERNAL_ERROR;
      }
      return new Response(withMemoryMeta(rawResult, core.memory, diagnostics), {
        status: 200,
        headers: {
          "cache-control": "no-store",
          "content-type": "application/json; charset=utf-8",
        },
      });
    } catch {
      return errorResponse(500, "internal error");
    }
  },
};
