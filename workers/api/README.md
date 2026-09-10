# Validation Worker

This directory contains the issue #9 validation Worker.
It sends JSON through the standard Go `js/wasm` runtime glue to the existing Unihan and CJClassifier core, then returns the existing `GateResult` shape.
The local CLI is unchanged.

## Current decision

**Insufficient evidence: STOP.**

The packed validation version is `2efc5d35-f637-425c-901a-d9063abc100f`, with a 33,695.45 KiB uncompressed upload and 20 ms global startup.
Downloaded deployed JS and Wasm hashes match `dist/packed/build-manifest.json`; the version served 100% of traffic at the beginning and end of verification.
All 200 validation requests returned the expected status. Eighteen comparisons matched the independent native legacy CLI, excluding only the four `validation_*` runtime diagnostic fields; authorization and malformed-input checks passed 13/13.

Across 169 instrumented requests, 27 were cold and all cold client times were below 10 seconds (maximum 5,095.85 ms). Cold 1 KiB client median was 1,796.82 ms (n=3); its one matched CPU sample was 1,123 ms. Warm 1 KiB CPU median was 3 ms (n=6).
Live tail matched 109/169 requests; 60 CPU/wall records were missing, including the final 54-request series. These are HTTP successes with incomplete CPU evidence. The collector did not persist transport error/close details, so diagnose that loss before increasing the CPU sample budget.

GraphQL observed maximum V8 isolate memory of 107,176,215 bytes and Wasm memory of 89,653,248 bytes in the recorded version/time window. These invocation observations are not continuous total peak memory.
Cold 100 KiB, waiting initialization, overlapping requests inside one isolate, and sufficient cold/warm CPU samples remain unverified. One concurrent group reused an isolate for two warm requests, but their recorded execution intervals did not overlap.
Issue #9 remains unresolved and issue #10 remains blocked. The CPU target below 1,000 ms is not met by the one observed cold 1 KiB CPU sample; it is a separate optimization target from the original feasibility gates.
See [`FEASIBILITY.md`](./FEASIBILITY.md) and [`feasibility.json`](./feasibility.json) for exact scopes, counts, missing records, and historical legacy measurements.

The Worker retains `GOMEMLIMIT=48MiB` and returns isolate ID, request sequence, `cold`/`waiting`/`warm`, and sampled Wasm capacity metadata for authenticated successful requests.

## Reproduce the local path

Run from `workers/api`:

```sh
npm ci
npm run build
npm run typecheck
npm test
```

`npm run build` verifies the generated CJ artifact, selects `jpqg_packed_cjmodel`, compiles the standard Go/Wasm entry point and runs Wrangler's dry-run bundling with `wrangler.validation.jsonc`.
It does not deploy anything.
`npm ci` installs the dependencies pinned in the committed lockfile.
`npm test` is the package's configured test command (`node --test test/*.test.mjs`).
The focused HTTP command used for the reported scoped result is:

```sh
node --test test/http.test.mjs
```

The earlier baseline verification reported by the parent session was clean-environment `make check` PASS (Go tests and vet, OMP 6/6, Pi 9/9), Wasm `go vet` PASS, two consecutive `npm run build` passes at the same `13,076.41 KiB`, `npm run typecheck` PASS, and `npm test` 8/8 PASS.
These are local checks and do not establish deployed Worker behavior.

The build creates ignored local artifacts under `.generated/packed/` and `dist/packed/`.
`JPQG_CJ_VARIANT=legacy npm run build` creates a separate legacy bundle under `.generated/legacy/` and `dist/legacy/`; it does not overwrite the packed deployment entry.
The validation configuration points to `.generated/packed/index.ts`, so build before running Wrangler.
Each output directory contains `build-manifest.json` with model, module, runtime glue, config, toolchain, and base commit provenance.
A dirty working tree is identified explicitly. Deployment/version evidence must be joined separately.
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

After the packed validation build has explicitly been deployed and its version ID recorded, run the same synthetic cases against it with the existing secret supplied through `JPQG_API_TOKEN`:

```sh
JPQG_CJ_VARIANT=packed JPQG_MEASURE_BUILD_MANIFEST=dist/packed/build-manifest.json \
  JPQG_DEPLOYED_VERSION="<recorded-version-id>" \
  JPQG_MEASURE_URL=https://jp-quality-gate-validation.ktutumi.workers.dev/v1/check \
  npm run --silent measure > .generated/deployed-measurement.json
```

The optional build manifest and expected version are operator-supplied claims; CPU and Worker wall time remain null until matched with live tail. The endpoint is recorded without URL credentials or query parameters.
The remote variant label is operator-supplied (or `unverified` if omitted), not proof of the deployed model; correlate every measurement ID with the expected live-tail version.
The remote command does not deploy or alter secrets, and fails if initialization metadata is absent.
It records client elapsed time, concurrency, initialization state, isolate ID, sequence, and sampled Wasm capacity, without saving request bodies, headers, or tokens.
Each request sends a random `x-jpqg-measurement` header and records its value as `measurement_id` for correlation with live tail; do not save raw tail headers containing the Bearer token.
Remote requests may reach different isolates; inspect IDs and states rather than assuming sequential requests are warm.
The runner itself leaves CPU and total peak isolate memory `null`: join request-correlated live tail CPU separately, and obtain evidence covering total peak memory before accepting feasibility.
It emits JSON even on an in-run failure (`completed: false`) and exits nonzero; preserve that partial record. `completed: true` means the HTTP checks finished, not that all feasibility criteria passed.
Concurrent requests all settle before results are emitted. HTTP status mismatches and transport failures are retained without saving response bodies or credentials.
The runner uses a streaming body for raw overflow, matching the HTTP tests; an early Content-Length rejection can close the upload connection and race with a subsequent request's connection reuse in the local client.

Choose `JPQG_MEASURE_FIRST=1024|10240|102400|262144|escaped|concurrent` to change the first case in each fresh local process. The runner also checks kana-free CJK, many findings, Markdown/astral text, warm sizes, boundaries, and four distinct concurrent inputs.
Run each first-case/variant combination five times sequentially, without other builds or tests running. Keep all emitted JSON, including failures.
Timing fields are cumulative milliseconds from request start to response headers, full body, and JSON parse; `client_elapsed_ms` additionally includes successful result validation. A single body read is used.
Headers time includes connection, upload and server wait; it is not isolated server CPU or pure TTFB.

For a separate local CDP profile of the first case:

```sh
JPQG_CJ_VARIANT=packed JPQG_MEASURE_PROFILE=1 npm run --silent measure > .generated/packed-profile-run.json
```

The runner saves `.generated/packed/cold-1024.cpuprofile` (variant/first-case dependent). Profiling is rejected for remote endpoints and its results must not be mixed with ordinary timing runs. It does not insert timers, sleeps or extra I/O into the production Worker.

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
The packed deployed bundle is `33,695.45 KiB`; the historical legacy upload was `13,076.83 KiB`.
`packed_model_measurements` records the local packed comparison; its `deployed_verification` section records the verified packed deployment and 200-request validation.
The `improvement_measurements` section preserves the controlled local comparison; `improvement_measurements.deployed_verification` records the new deployed startup, HTTP, and correlated CPU results. Total peak memory and the cold latency condition remain unresolved.

## Packed artifact maintenance

The packer requires canonical language columns in order, and records paths relative to its working directory; paths outside that directory are rejected.
Run `make pack-cj` from the repository root after an intentional source/parser/format change, review the binary manifest, then run `make check`.
Normal builds never regenerate the model. The lightweight Worker check rejects missing files, changed source/file/content hashes, and inconsistent manifests; complete regeneration additionally detects stale parser output.
`make check-cj-packed` tests bootstrap in a disposable source copy and verifies exclusive embed selection. The ordinary native CLI keeps its legacy loader.

The packed local comparison passed bitwise model parity and HTTP parity against the independently built legacy CLI. Five fresh cold runs per case and variant showed a cold 1 KiB median of 1,172.88 → 376.12 ms and sampled Wasm capacity maxima of 101,711,872 → 91,226,112 bytes.
The later deployed verification confirms startup and a limited set of CPU samples; continuous total peak memory and Issue #9 feasibility remain unresolved. See the detailed record.
