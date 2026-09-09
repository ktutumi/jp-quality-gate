# Validation Worker

This directory contains the issue #9 validation Worker.
It sends JSON through the standard Go `js/wasm` runtime glue to the existing Unihan and CJClassifier core, then returns the existing `GateResult` shape.
The local CLI is unchanged.

## Current decision

**Insufficient evidence: STOP.**

The deployed validation version is `820f287a-573d-4b9c-925d-5021640dd2eb`, with a 13,076.83 KiB upload and 35 ms global startup.
Deployed results matched the native CLI in 18/18 comparisons, and authorization, malformed inputs, body boundaries, and concurrent distinct inputs returned the expected results.
After OAuth renewal, 13 more HTTP requests were correlated with live tail CPU and wall time for this version; warm 1 KiB CPU was 3 ms.
The earlier authentication failure is resolved for the current OAuth session.

The GraphQL invocation memory maximum was 99,435,263 bytes for this version over the recorded two-hour window.
This aggregate is not proof of continuous total peak isolate memory.
Cold client elapsed time still exceeded 10 seconds: the latest longest request took 26,058.03 ms, while its corresponding Worker wall time was 6,245 ms.
Four concurrent requests reached distinct isolates; same-isolate concurrent execution was not observed remotely.
Issue #9 remains unresolved and issue #10 remains blocked.
See [`FEASIBILITY.md`](./FEASIBILITY.md) and [`feasibility.json`](./feasibility.json) for exact scopes, timestamps, and historical measurements.

The deployed Worker sets `GOMEMLIMIT=48MiB` in the standard Go runtime and returns isolate ID, request sequence, and `cold`/`waiting`/`warm` initialization metadata for authenticated successful requests.
A controlled local comparison reduced sampled Wasm capacity; two further parser optimization trials regressed Wasm memory and were reverted without deployment.

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

For a future explicitly authorized validation deployment, use only the validation configuration:

```sh
npx wrangler secret put JPQG_API_TOKEN --config wrangler.validation.jsonc
npx wrangler deploy --config wrangler.validation.jsonc
```

The user has already deployed the validation endpoint; do not repeat these commands merely to run HTTP checks or collect live logs.
For deployed checks, use `https://jp-quality-gate-validation.ktutumi.workers.dev/v1/check` and the supplied `JPQG_API_TOKEN`, not the disposable local token.
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

For a repeatable HTTP measurement using a fresh local Miniflare/workerd instance:

```sh
npm run build
npm run --silent measure > .generated/local-measurement.json
```

After the instrumented validation build has been deployed, run the same synthetic cases against it with the existing secret supplied through `JPQG_API_TOKEN`:

```sh
JPQG_MEASURE_URL=https://jp-quality-gate-validation.ktutumi.workers.dev/v1/check \
  npm run --silent measure > .generated/deployed-measurement.json
```

The remote command does not deploy or alter secrets, and fails if initialization metadata is absent.
It records client elapsed time, concurrency, initialization state, isolate ID, sequence, and sampled Wasm capacity, without saving request bodies, headers, or tokens.
Each request sends a random `x-jpqg-measurement` header and records its value as `measurement_id` for correlation with live tail; do not save raw tail headers containing the Bearer token.
Remote requests may reach different isolates; inspect IDs and states rather than assuming sequential requests are warm.
The runner itself leaves CPU and total peak isolate memory `null`: join request-correlated live tail CPU separately, and obtain evidence covering total peak memory before accepting feasibility.
It emits JSON even on an in-run failure (`completed: false`) and exits nonzero; preserve that partial record. `completed: true` means the HTTP checks finished, not that all feasibility criteria passed.
Concurrent requests all settle before results are emitted. HTTP status mismatches and transport failures are retained without saving response bodies or credentials.
The runner uses a streaming body for raw overflow, matching the HTTP tests; an early Content-Length rejection can close the upload connection and race with a subsequent request's connection reuse in the local client.

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
The current dry-run bundle value is `13,076.83 KiB`; older CPU and heap profiles and deployed results describe earlier builds.
The `improvement_measurements` section preserves the controlled local comparison; `improvement_measurements.deployed_verification` records the new deployed startup, HTTP, and correlated CPU results. Total peak memory and the cold latency condition remain unresolved.
