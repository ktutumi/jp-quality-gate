# Validation Worker

This directory contains the issue #9 validation Worker.
It sends JSON through the standard Go `js/wasm` runtime glue to the existing Unihan and CJClassifier core, then returns the existing `GateResult` shape.
The local CLI is unchanged.

## Current decision

**Insufficient evidence: STOP.**

The measurements in [`feasibility.json`](./feasibility.json) are local Miniflare/workerd observations only.
Cloudflare deployment measurements could not be collected: the default Wrangler OAuth token had expired and refresh failed, while a separately available credential verified as active but lacked usable Workers access to the configured validation account and returned `403 Authentication error`.
Issue #9 remains unresolved, and downstream issue #10 remains blocked.
No real Worker pass, same-isolate concurrency guarantee, billed CPU result, startup result, or 128 MB peak-isolate-memory result is claimed here.

## Reproduce the local path

Run from `workers/api`:

```sh
npm ci
npm run build
npm run typecheck
npm test
```

`npm run build` compiles the standard Go/Wasm entry point and runs Wrangler's dry-run bundling with `wrangler.validation.jsonc`.
It does not deploy anything.
`npm ci` installs the dependencies pinned in the committed lockfile.
`npm test` is the package's configured test command (`node --test test/*.test.mjs`).
The focused HTTP command used for the reported scoped result is:

```sh
node --test test/http.test.mjs
```

Final local verification reported by the parent session is clean-environment `make check` PASS (Go tests and vet, OMP 6/6, Pi 9/9), Wasm `go vet` PASS, two consecutive `npm run build` passes at the same `13,076.41 KiB`, `npm run typecheck` PASS, and `npm test` 8/8 PASS.
These are local checks and do not establish deployed Worker behavior.

The build creates ignored local artifacts under `.generated/` and `dist/`.
The generated `wasm_exec.js` is copied without modification from the installed Go distribution selected by `go env GOROOT`; the corresponding Go `LICENSE` is copied alongside it.

## Local server and token

`wrangler.validation.jsonc` is the only Worker configuration in this directory.
It is validation-only and is not a production configuration.
Keep the Bearer token out of that file and out of command history.

For a local session, create a disposable token in the shell and let Wrangler import the process environment (do not create a secret file):

```sh
export JPQG_API_TOKEN="$(node -e 'process.stdout.write(require("node:crypto").randomBytes(32).toString("hex"))')"
CLOUDFLARE_INCLUDE_PROCESS_ENV=true \
  npx wrangler dev --local --config wrangler.validation.jsonc
```

The command runs local workerd/Miniflare on `http://localhost:8787`.
Do not add `--tunnel` or otherwise expose this validation endpoint publicly.
Use the same token from a separately managed shell or secret store when sending requests.
Do not use the fixed token used by the test fixture as a production secret.

If a deployed validation measurement is later authorized, set the secret and deploy only the validation configuration, each time naming it explicitly:

```sh
npx wrangler secret put JPQG_API_TOKEN --config wrangler.validation.jsonc
npx wrangler deploy --config wrangler.validation.jsonc
```

Those commands require valid Cloudflare authentication and were not executed because the configured account's Workers API returned 403.
They must not be interpreted as a production deployment procedure.

The local development and environment-variable guidance follows the [Wrangler worker commands](https://developers.cloudflare.com/workers/wrangler/commands/workers/), [Cloudflare local development](https://developers.cloudflare.com/workers/local-development/), and [Cloudflare environment variables](https://developers.cloudflare.com/workers/configuration/environment-variables/) documentation.

## HTTP contract

Send `POST /v1/check` with `Content-Type: application/json` and an exact `Authorization: Bearer <token>` header:

```sh
curl --fail-with-body -sS \
  -X POST http://127.0.0.1:8787/v1/check \
  -H 'content-type: application/json' \
  -H "authorization: Bearer ${JPQG_API_TOKEN}" \
  --data '{"text":"これは経済に関する説明です。","options":{"cj_min_cjk":4,"cj_min_gap":0.15,"include_code":false,"warnings_as_errors":false}}'
```

The request object has this shape:

| Field | Requirement | Accepted values |
| --- | --- | --- |
| `text` | required | string; decoded UTF-8 length at most 262,144 bytes |
| `options` | optional | object containing only the four keys below |
| `options.cj_min_cjk` | optional | safe integer at least `1`; default `4` |
| `options.cj_min_gap` | optional | finite number from `0` through `1`; default `0.15` |
| `options.include_code` | optional | boolean; default `false` |
| `options.warnings_as_errors` | optional | boolean; default `false` |

Issue #9 does not support a `checks` selector.
Unknown top-level fields and unknown option fields are rejected with `400`; do not send `checks` to select Unihan or CJClassifier independently.
The bridge invokes the existing core with the normalized request and returns its `GateResult`, including runtime-only metadata for local diagnostics.

The raw request body is capped at `6 * 256 KiB + 4096 = 1,576,960` bytes.
The decoded `text` limit is exactly `256 KiB = 262,144` bytes.
The body is read as UTF-8 JSON without truncation or splitting; an over-limit body or decoded text returns `413` before core processing.
Missing or incorrect Bearer credentials return `401`.
Malformed JSON, invalid UTF-8, an invalid schema, or a non-JSON content type returns `400`.
Internal bridge or core failures return a generic `500` envelope.
The handler does not log the body or token, and error responses do not echo the token.

## Local profiling

Build first, then start Wrangler with its inspector endpoint:

```sh
CLOUDFLARE_INCLUDE_PROCESS_ENV=true \
  npx wrangler dev --local \
  --config wrangler.validation.jsonc \
  --inspector-port 9229
```

Open `chrome://inspect`, attach to the Worker target, and use the DevTools **Performance** panel to record a fresh process and then warm requests.
Record cold 1 KiB, warm 1/10/100/256 KiB, empty text, a decoded 256 KiB boundary, an over-limit body, and four concurrent 256 KiB requests with distinct inputs.
Save raw profiles under `.generated/`; generated artifacts are ignored.
The procedure follows the [Wrangler `dev` options](https://developers.cloudflare.com/workers/wrangler/commands/workers/#dev) and [Chrome DevTools Performance reference](https://developer.chrome.com/docs/devtools/performance/reference).

`local_profile_nonidle_sample_ms` is a sampled local runtime signal, not Cloudflare billed CPU.
A zero non-idle sample does not establish zero CPU usage.
`wasm_linear_memory_bytes` and the V8 heap samples are separate local observations, not a peak-isolate measurement.

## Evidence record

[`FEASIBILITY.md`](./FEASIBILITY.md) contains the compact measurement tables, exact boundary and concurrency caveats, failed startup profiling attempts, deployment blocker, glossary, and STOP decision.
[`feasibility.json`](./feasibility.json) preserves the full numeric record.
The latest dry-run bundle value is `13,076.41 KiB`; the CPU and heap profiles remain from the earlier normal-path measurement run and were not remeasured after the error-path-only fix.
