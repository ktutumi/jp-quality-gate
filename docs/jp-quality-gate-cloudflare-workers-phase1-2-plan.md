# jp-quality-gate Cloudflare Workers API — Phase 1〜2 実装計画

> 更新日時: 2026-09-09 19:40

作成日: 2026-09-09

対象: https://github.com/ktutumi/jp-quality-gate

## 0. スコープ

この計画は次の2段階だけを対象にする。

- **Phase 1:** Unihan + CJClassifier を Cloudflare Workers の HTTP API として提供
- **Phase 2:** textlint を Workers 内で in-process 実行
- **Phase 3:** natural-japanese は対象外

現在の pure Go CLI は残し、ローカル/オフライン用途の標準経路として維持する。

Phase 1〜2 は、自分が管理する OMP/Pi と端末から使う個人用 API に限定する。
一般公開、チーム向け利用者管理、課金機能は含めない。
API モードの明示選択を検査対象全文の Cloudflare への送信に対する同意とする。
送信禁止の用途は local モードで運用し、文章の内容による自動振り分けは実装しない。

Phase 1 は単独で公開できる。
Phase 2 が成立しなければ停止して配置先を再判断し、その間 textlint が必要な用途は local を使う。
ルールを黙って削減して「同等」と扱わない。

本書は設計合意を反映した実装前の計画であり、Workers での成立性や受け入れ条件の達成を示すものではない。
用語は [CONTEXT.md](../CONTEXT.md) に従う。

---

## 1. ゴール

最終形:

```text
OMP / Pi / 任意クライアント
        |
        | HTTPS
        v
Cloudflare Worker (TypeScript)
        |
        +-- Phase 1
        |    +-- Unihan
        |    +-- CJClassifier
        |
        +-- Phase 2
             +-- textlint
```

目的:

- 複数マシンから同じ品質基準を利用
- local CLI と remote API を切替可能にする
- Workers 内では subprocess を使わない
- 現在の `GateResult` / `issues[]` schema を維持
- OMP/Pi の既存自動修正ループを再利用
- API側で品質基準を集中管理

品質基準は、ルール、辞書、モデル、設定の組合せを指す。
同じ入力と品質基準で local/API の結果を一致させ、API を使う端末間はサーバーの基準で統一する。
古い CLI との永続的一致は保証しない。
品質基準バージョンは API の形式バージョンとは別に識別し、結果の `meta` に含める。
結果互換性の比較範囲は §15、更新中の修正ループの扱いは §16 に定める。

---

## 2. 現在の実装を基準にする

現在の `main` は pure Go 実装で、次のデータをバイナリに埋め込んでいる。

```text
internal/embedded/data/cjlogprobs.gz
internal/embedded/data/unihan-suspicious-18.0.0.json.gz
```

Phase 2 の local textlint は現時点で次を使用している。

```text
textlint                                           15.8.0
@textlint-ja/textlint-rule-preset-ai-writing       1.7.0
textlint-rule-preset-ja-technical-writing          12.0.2
```

設定:

```json
{
  "rules": {
    "preset-ja-technical-writing": true,
    "@textlint-ja/preset-ai-writing": {
      "ai-tech-writing-guideline": {
        "severity": "info"
      }
    }
  }
}
```

Workers版もこの挙動との parity を目標にする。

### 実装照合で確認した境界

| 現行実装 | 計画への反映 |
| --- | --- |
| [`Gate.Check`](../internal/gate/gate.go) は Unihan と CJ を常に実行 | 個別チェック選択は新規 API 契約。local の既定動作は変更しない |
| [`GateResult`](../internal/report/report.go) は `Issues` と `Meta` を保持し、JSON 化時に判定と集計を算出 | 正常応答は既存の派生 JSON 形式を維持する |
| 同ファイルの `Normalize` は warning 昇格後、位置順と同位置での error 優先による安定ソート | §29 の結合後にも同じ結果を維持する |
| [`prose.Check`](../internal/prose/prose.go) は明示的な textlint 設定を要求し、URL を mask してコードは保持 | API 固定設定との違いを §23、masking を §28 に記録 |
| 同ファイルの `issue` は外部指摘を warning にし、`parseTextlint` は UTF-16 位置を code-point に変換 | preset の info をそのまま API に出さず、位置と重要度の結果互換性を維持 |
| [`OMP`](../integrations/omp/index.js) と [`Pi`](../integrations/pi/index.js) は local process を実行し、正常結果の `summary` と `issues` を検証 | remote transport を追加し、API エラーを正常結果 validator に渡さない |

この照合はソースの確認であり、Go/Wasm や textlint の Workers 実行を検証したものではない。

---

## 3. Cloudflare Workers の前提

2026-09-09 時点:

```text
Worker size      64 MiB (uncompressed)
isolate memory   128 MB
startup time     1 sec
Free CPU         10 ms/request
Paid CPU         default 30 sec, configurable up to 5 min
```

Workers は precompiled WebAssembly をサポートする。

公式の [Limits](https://developers.cloudflare.com/workers/platform/limits/) を確認した。
64 MiB は非圧縮サイズで、dry-run 出力の `Total Upload` を使う。
1秒はグローバルスコープの解析と実行の上限であり、handler 内の遅延初期化を含む初回応答時間ではない。
デプロイまたは version upload の `startup_time_ms` と、初回リクエストの CPU 時間および経過時間を分けて記録する。
128 MB はリクエスト単位ではなく isolate 単位で、JavaScript heap と Wasm の割当を含む。
同じ isolate が複数リクエストを処理するため、単発実行だけではメモリの検証にならない。

本番は **Workers Paid** 前提を推奨する。

Free の 10 ms CPU は CJClassifier の cold initialization や Phase 2 textlint を含む用途には厳しい。

参考:

- https://developers.cloudflare.com/workers/platform/limits/
- https://developers.cloudflare.com/workers/languages/
- https://developers.cloudflare.com/workers/runtime-apis/webassembly/
- https://developers.cloudflare.com/workers/runtime-apis/webassembly/javascript/

---

## 4. 推奨アーキテクチャ

まずは **1 Worker構成**にする。

```text
Cloudflare Worker (TypeScript)
  |
  +-- HTTP / auth / validation
  |
  +-- Core bridge
  |    |
  |    +-- Go -> Wasm
  |         +-- Unihan
  |         +-- CJClassifier
  |
  +-- Phase 2
       +-- @textlint/kernel
```

128 MB memory / startup / bundle size に問題が出た場合は停止して配置先を再判断する。
Service Binding による分割は、その再判断で検討できる候補であり、自動的な次工程ではない。

```text
API Worker
  +-- service -> Core Worker
  +-- service -> Textlint Worker
```

最初から microservice 化しない。

---

## 5. リポジトリ構成案

```text
jp-quality-gate/
├── cmd/
│   ├── jp-quality-gate/
│   ├── jpqg-build-unihan/
│   └── jp-quality-gate-wasm/
├── internal/
├── integrations/
│   ├── omp/
│   └── pi/
├── tools/
│   └── textlint/
└── workers/
    └── api/
        ├── src/
        │   ├── index.ts
        │   ├── api/
        │   │   ├── auth.ts
        │   │   ├── check.ts
        │   │   └── schema.ts
        │   ├── core/
        │   │   └── bridge.ts
        │   └── textlint/
        │       ├── lint.ts
        │       ├── rules.ts
        │       └── normalize.ts
        ├── test/
        ├── package.json
        ├── tsconfig.json
        └── wrangler.jsonc
```

---

# Phase 0 — Architecture Spike

Phase 1 の前に Go core の Wasm 化を検証する。

## 第一候補

```text
GOOS=js
GOARCH=wasm
```

WASI は使わない。

TypeScript Worker から precompiled Wasm を instantiate する。

## Wasm boundary

細かい function を大量に公開せず、JSON 1往復にする。

```text
check(requestJSON) -> responseJSON
```

Go CLI の parser を呼ばず、既存の:

```text
internal/gate
internal/unihan
internal/cj
internal/report
internal/text
internal/embedded
```

を直接利用する。

CLI と `internal/prose` の subprocess、設定ファイル読込経路は Wasm に取り込まない。
[`cj.Load`](../internal/cj/classifier.go) の埋込モデル読込と [`unihan.LoadBytes`](../internal/unihan/unihan.go) のようなインメモリ経路を使用する。
標準 Go の JS/Wasm runtime glue を含め、Workers との互換性は Phase 0 で実証する。

## 初期化

CJClassifier model を request ごとに parse しない。

```ts
let corePromise: Promise<Core> | undefined;

function getCore() {
  return corePromise ??= initializeCore();
}
```

isolate ごとに一度だけ lazy initialize する。

## Spike acceptance

必須:

```text
uncompressed bundle       < 64 MiB
global-scope startup      < 1 sec
per-isolate memory        < 128 MB
```

§17 の検証用 Worker で初回と再利用時、上限入力、同時実行を検証する。
遅延初期化によって startup が短くなっただけでは合格としない。
測定方法と観測できる範囲を記録し、測定できない必須項目を合格扱いにしない。

推奨目標:

```text
bundle       < 32 MiB
startup      < 500 ms
warm 1KB     < 50 ms CPU
```

## Fallback

標準 Go/Wasm が size/startup/memory/compatibility で成立しなければ、Phase 0 で停止する。
測定結果を基に、既存 Go を動かせる配置先を含めて再判断する。
Workers 固定のために Rust/Wasm または TypeScript への移植を自動的に開始しない。
Service Binding 分割や追加の最適化も、再判断後に必要と認めた範囲に限る。
local Go CLI は変更しない。

---

# Phase 1 — Unihan + CJClassifier API

## 6. HTTP API

### Endpoint

```http
POST /v1/check
Authorization: Bearer <token>
Content-Type: application/json
```

### Request

```json
{
  "text": "これは经済に関する説明です。",
  "checks": {
    "unihan": true,
    "cj": true,
    "textlint": false
  },
  "options": {
    "cj_min_cjk": 4,
    "cj_min_gap": 0.15,
    "include_code": false,
    "warnings_as_errors": false
  }
}
```

defaults:

```text
unihan=true
cj=true
textlint=false
cj_min_cjk=4
cj_min_gap=0.15
include_code=false
warnings_as_errors=false
```

既定値を適用した後、少なくとも1つの検査を有効にする。
すべて `false` の要求は400にする。
Phase 1 の `checks.textlint=true` は未対応として400にし、黙って無視しない。
空文字列は有効な `text` として受け付けるが、少なくとも1つの検査を選択する条件は維持する。

---

## 7. Response

既存 GateResult を維持する。

```json
{
  "pass": false,
  "summary": {
    "errors": 1,
    "warnings": 0,
    "issues": 1
  },
  "issues": [
    {
      "rule": "simplified_chinese_form",
      "severity": "error",
      "message": "...",
      "start": 3,
      "end": 4,
      "text": "经",
      "line": 1,
      "column": 4,
      "details": {
        "codepoint": "U+7ECF",
        "japanese_candidates": ["経"]
      }
    }
  ],
  "meta": {
    "api_version": "v1",
    "quality_criteria_version": "<品質基準の組合せを識別する版>",
    "engine": "cloudflare-workers"
  }
}
```

品質 pass/fail は HTTP status では表現しない。

```text
200 = gate 実行成功。pass true/false はbodyで表現
400 = request schema error
401 = auth error
413 = application input limit
500 = internal error
503 = initialization/transient error
```

品質エラーでも HTTP 200。
要求した検査がすべて正常に完了した場合だけ、§7 の GateResult を返す。
Unihan/CJ が成功しても textlint が実行不能なら、要求全体を検査不能として非200にする。
成功した検査だけの部分結果は返さず、それを基にした自動修正も行わない。

---

## 8. API error schema

```json
{
  "pass": false,
  "internal_error": "..."
}
```

このエラー形式は正常な GateResult ではなく、既存 OMP/Pi adapter の schema validator には渡さない。
両 adapter は正常結果に `summary` と `issues` を必須としている。
remote transport は非200応答を先に検査不能（local exit 2 相当）へ分類する。
HTTP 200 でも JSON 不正、schema 不正、判定と集計の矛盾があれば検査不能にする。
`pass:false` だけを見て品質不合格として扱わない。
Cloudflare 側のエラーなど JSON でない非200応答も同じ分類にする。

---

## 9. 認証

MVP は Bearer secret。

```http
Authorization: Bearer <JPQG_API_TOKEN>
```

登録:

```bash
wrangler secret put JPQG_API_TOKEN
```

Phase 1〜2 では Cloudflare Access / mTLS は必須にしない。

厳密な月額停止保証は要件にしない。
利用量と費用を監視し、漏えいや異常な呼出しを認めた場合は Bearer token を失効させる運用とする。
独自の利用量台帳や課金制御は実装しない。
入力サイズ制限は呼出し回数や月額費用の上限を保証しない。

---

## 10. Input limit

アプリ側で:

```text
text <= 256 KiB UTF-8
```

を初期上限とする（JSON デコード後の `text` の UTF-8 バイト数）。
超過は413で拒否し、自動切り詰めも分割も行わない。
OMP/Pi は検査不能を通知し、現在の応答をそのまま返す。
この上限で安全に処理できることは未検証であり、§17 の上限入力検証を公開条件とする。

目的:

- 128 MB memory 保護
- textlint AST 暴走防止
- abuse防止

---

## 11. Wasm core entrypoint

新規:

```text
cmd/jp-quality-gate-wasm/
```

責務:

- request JSON parse
- option validation
- engine initialization
- GateResult JSON serialization

HTTP/auth は TypeScript 側。

---

## 12. Core stage selection

Workers API は:

```json
"checks": {
  "unihan": true,
  "cj": true
}
```

を扱う。

local CLI の default behavior は両方 true のまま維持する。
既存 `internal/gate.Check` は両 scanner を常に実行するため、個別選択は新しい API 契約として実装する。
無効な検査は実行せず、API の全無効要求は §6 のとおり拒否する。
OMP/Pi には今回チェック選択用の新しい環境変数を追加せず、core は両方有効、`include_code=false` を維持する。

---

## 13. Health

```http
GET /healthz
```

```json
{
  "ok": true,
  "version": "..."
}
```

health check では heavy model initialization を強制しない。

---

## 14. Security

必須:

- Bearer secret
- input size guard
- `Cache-Control: no-store`
- stack trace を client に返さない
- token をログしない
- LLM response 本文をログしない

ログは metadata のみにする。

```json
{
  "event": "jpqg.check",
  "request_id": "...",
  "checks": ["unihan", "cj"],
  "pass": false,
  "errors": 1,
  "warnings": 0,
  "duration_ms": 12
}
```

---

## 15. Conformance tests

local Go CLI と Workers API の結果を同一 corpus で比較する。

同じ入力と品質基準を固定し、次を比較する。

```text
pass
summary
issues 全体（配列順序を含む）
  rule
  severity
  message
  start
  end
  text
  line
  column
  details
```

指摘を集合として比較したり、説明文を比較から外したりしない。
実行時間、実行環境名など runtime 固有の `meta` は比較対象から除外する。
比較に使った品質基準の版、選択した検査、オプションを記録する。
local CLI にない個別チェック選択は既存 scanner を期待値の基準として検証し、CLI に新しい選択フラグを追加しない。

fixture:

```text
正常日本語
簡体字混入
Simplified Chinese
Traditional Chinese
日本語 + 中国語 clause
fenced code
inline code
URL
emoji
CJK Extension
warnings-as-errors
長文
```

例:

```text
これは経済に関する説明です。
これは经済に関する説明です。
今天天气很好，我们去公园散步。
今天天氣很好，我們去公園散步。
🙂🙂これは经済です。
```

Unicode code-point offset parity を必須にする。

---

## 16. OMP / Pi remote backend

Phase 1 完了範囲に含める。

追加 env:

```text
JPQG_BACKEND=local|api
JPQG_API_URL=https://jp-quality-gate.example.com
JPQG_API_TOKEN=...
JPQG_API_TIMEOUT_MS=10000
```

default:

```text
JPQG_BACKEND=local
```

既存挙動を変えない。

概念:

```js
async function runGate(text) {
  return backend === "api"
    ? runRemoteGate(text)
    : runLocalGate(text);
}
```

remote mapping:

```text
HTTP 200 + valid GateResult + pass=true   -> local exit 0 相当
HTTP 200 + valid GateResult + pass=false  -> local exit 1 相当
HTTP != 200 / invalid result / transport error -> local exit 2 相当
```

API failure は既存どおり fail-open とするが、品質合格とは区別する。
検査不能を通知し、現在の応答を返す。
自動の local 再実行は行わない。

`JPQG_API_TIMEOUT_MS` は1回の API 呼出しについて応答本文の取得までを含む期限で、既定は10,000 ms。
タイムアウト、認証エラー、非200、不正な応答は通信の自動再試行をせず、直ちに検査不能として扱う。
品質不合格による修正と再検査は通信再試行と区別する。

`JPQG_MAX_RETRIES` の既定2回、範囲0〜7を維持する。
0回なら品質不合格を報告するだけで修正しない。
上限到達時は不合格が残っていることを通知し、最後の応答を返す。
追加修正や応答の非表示は行わず、返却を品質合格として扱わない。

各検査はその時点のサーバー基準で実行し、結果の `meta.quality_criteria_version` で版を識別する。
修正ループ中に品質基準が更新されても、修正回数をリセットしない。
ループをまたぐ版固定は保証せず、旧版を指定して実行する機能は作らない。

---

## 17. Phase 1 deployment

`workers/api/wrangler.jsonc` の概念:

```json
{
  "name": "jp-quality-gate-api",
  "main": "src/index.ts",
  "compatibility_date": "2026-09-09",
  "observability": {
    "enabled": true
  }
}
```

実際の schema は使用する Wrangler 最新版に合わせる。

公開前のローカル確認:

```bash
cd workers/api
npm ci
npm test
npm run typecheck
npx wrangler deploy --dry-run
```

この結果だけでは公開可にしない。
別途用意した検証用 Worker の設定を明示してデプロイし、以下を確認してから本番へ公開する。
検証用の環境設定と secret は実装時に用意し、本書の本番向け設定例をそのまま検証用として実行しない。

| 検証対象 | 証拠と合格条件 |
| --- | --- |
| 配置サイズ | dry-run の非圧縮 `Total Upload` が64 MiB未満 |
| グローバル初期化 | デプロイまたは version upload の `startup_time_ms` が1,000 ms未満 |
| 初回リクエスト | 遅延初期化を含む CPU 時間と経過時間を記録し、既定10秒の adapter 期限内に正常結果を取得 |
| 再利用時 | 1 KiB、10 KiB、100 KiB、256 KiBの入力で結果と CPU 時間、経過時間を記録。warm 1 KiB は50 ms CPU未満を推奨目標とする |
| 上限と境界 | UTF-8で256 KiBちょうどは正常処理、超過は413、空文字列は有効。切り詰めや分割をしていないことを確認 |
| 同時実行 | 異なる入力を重ね、初期化の競合、結果混入、実行エラーがないことと結果互換性を確認。実行した同時数を記録 |
| メモリ | JavaScript と Wasm を含む isolate 全体を対象に測定方法と観測範囲を記録し、128 MB未満を確認 |
| 結果互換性 | 同じ品質基準の local 結果と、実際の Worker の結果を §15 の範囲で比較 |
| adapter | 実際の OMP/Pi で合格への修正、修正上限での停止、検査不能時の応答継続を確認 |

公式資料で確認できるメモリ調査手段にはローカル DevTools の profiling がある。
本番相当の runtime 設定での profiling と検証用 Worker の同時実行結果を区別して記録し、エラーが出ないことだけから実環境の peak memory を断定しない。
単に同時リクエストを送ったことから、すべてが同一 isolate で処理されたとも断定しない。
必須項目について証拠が得られない場合は公開判定を保留し、測れない項目を合格扱いにしない。
Phase 2 では textlint を含む構成で同じ検証を繰り返す。

---

## 18. Phase 1 CI

```text
Go:
  go test ./...
  go vet ./...

Worker:
  npm ci
  npm test
  npm run typecheck
  wrangler deploy --dry-run

Adapters:
  OMP tests
  Pi tests

Conformance:
  local Go CLI vs Worker fixtures
```

CI の成功と §17 の実環境検証は別の条件であり、CI だけでは公開可にしない。

---

## 19. Phase 1 acceptance criteria

- [ ] `POST /v1/check`
- [ ] Bearer auth
- [ ] Unihan API
- [ ] CJClassifier API
- [ ] local Go CLI と corpus parity
- [ ] Unicode offsets parity
- [ ] Markdown/code/URL masking parity
- [ ] warnings_as_errors parity
- [ ] bundle < 64 MiB
- [ ] startup < 1 sec
- [ ] memory < 128 MB
- [ ] request size guard
- [ ] response本文をログしない
- [ ] OMP `JPQG_BACKEND=api`
- [ ] Pi `JPQG_BACKEND=api`
- [ ] API failure が fail-open
- [ ] local backend が default
- [ ] 全チェック無効と Phase 1 の textlint 指定が400
- [ ] 空文字列を受け付け、256 KiB超過は413（切り詰めと分割なし）
- [ ] 説明文と配列順序を含む `issues` 全体の結果互換性
- [ ] 正常結果に品質基準バージョンを含む
- [ ] 非200、不正な200応答、通信障害を検査不能として通知
- [ ] 本文取得まで既定10秒、通信再試行と自動 local 再実行なし
- [ ] 修正上限で不合格を通知して最後の応答を返す
- [ ] 基準更新によって修正回数をリセットしない
- [ ] §17 の検証用 Worker で初回、再利用、上限入力、同時実行を確認
- [ ] 利用量監視と異常時の token 失効手順を確認

---

# Phase 2 — textlint

## 20. 基本方針

Workers から textlint CLI を spawn しない。

使うのは:

```text
@textlint/kernel
```

textlint 公式はこれを browser / non-Node.js environment 向け low-level API として提供している。

参考:

- https://github.com/textlint/textlint
- https://github.com/textlint/textlint/blob/master/docs/plugin.md

---

## 21. Phase 2 dependencies

Workers package:

```text
@textlint/kernel
@textlint/textlint-plugin-markdown
textlint-rule-preset-ja-technical-writing
@textlint-ja/textlint-rule-preset-ai-writing
```

現行 local config と同じルールを first target にする。

Workers では CLI package `textlint` 全体を依存にせず、可能な限り kernel + 必要 package だけを bundle する。

---

## 22. Feasibility spike

まず preset 全体が Workers bundler で動くか確認。

調査:

```text
fs
path
dynamic require
process
Node-only API
bundle size
startup
```

合格:

```text
wrangler build/deploy 成功
lintText 成功
local textlint と fixture findings が一致
```

preset loader が制約になる場合は、同じルールと設定を維持した static import で成立性を確認する。
runtime `.textlintrc` 読込は行わない。
AI preset の依存には `kuromojin` があり、辞書パスや Node 向け処理を使うため、bundler の成功だけでなく辞書を使う実際の lint 実行を確認する。
これは依存調査で判明したリスクであり、Workers で実行不能と確認した結果ではない。

同じルール群の動作、§15 の結果互換性、§30 の実行制限が成立しなければ Phase 2 を停止し、配置先を再判断する。
ルールの削減や自動的な Worker 分割を次工程にしない。
Phase 1 の単独公開は妨げない。

---

## 23. Workers textlint config

MVP は server-side 固定設定。

```json
{
  "preset-ja-technical-writing": true,
  "@textlint-ja/preset-ai-writing": {
    "ai-tech-writing-guideline": {
      "severity": "info"
    }
  }
}
```

Phase 2 では任意 `.textlintrc` upload/path 指定や profile 選択を実装しない。
API モードでは `JPQG_TEXTLINT_CONFIG` と `JPQG_TEXTLINT_BIN` を使用せず、サーバー固定設定を適用する。
ローカル専用指定がある場合は、その指定が無視されることを adapter の初期化時に一度通知する。
指定された設定ファイルの送信や、パスのログ出力は行わない。
local モードの既存の設定要件と挙動は変更しない。

---

## 24. API request

```json
{
  "text": "...",
  "checks": {
    "unihan": true,
    "cj": true,
    "textlint": true
  },
  "options": {
    "warnings_as_errors": false
  }
}
```

`checks.textlint=true` で server-side config を有効化。

---

## 25. textlint execution

概念:

```ts
const kernel = new TextlintKernel();

const result = await kernel.lintText(text, {
  ext: ".md",
  plugins: [
    {
      pluginId: "@textlint/markdown",
      plugin: markdownPlugin
    }
  ],
  rules: bundledRules
});
```

導入時の `@textlint/kernel` API に合わせて実装する。

filesystem config loader は使わない。

---

## 26. Issue normalization

```json
{
  "rule": "textlint:<ruleId>",
  "severity": "warning",
  "message": "...",
  "start": 10,
  "end": 18,
  "text": "...",
  "line": 2,
  "column": 5,
  "details": {
    "source": "textlint",
    "rule_id": "<ruleId>"
  }
}
```

current local integration と同じ naming / severity policy に合わせる。

default:

```text
textlint finding = warning
```

`warnings_as_errors=true` の場合だけ error に昇格。
local では preset 内の `severity:info` を含め、外部 textlint の指摘を warning に正規化している。
API の指摘に新しい `info` severity は導入しない。
警告だけなら品質合格となり、自動修正を開始しない。
textlint の指摘を修正対象にするには `warnings_as_errors=true` を明示する。
新しい警告通知機能は追加しない。

---

## 27. Unicode position

JavaScript/textlint の offset は UTF-16 code unit になる可能性がある。

API schema は current Go と同じ Unicode code-point offset を維持する。

必ず変換:

```text
UTF-16 offset
 -> Unicode code point offset
```

test:

```text
🙂🙂不自然な文章
𠮷野家
emoji before finding
surrogate pair before finding
```

---

## 28. Markdown

prose lint の対象外:

```text
fenced code
inline code
URL
```

local の2つの経路を区別して維持する。

1. Unihan/CJ は URL を常に mask し、`include_code=false` のときだけ fenced code と inline code も mask する。
2. textlint は URL を mask し、コードを保持した入力を Markdown processor に渡す。
3. textlint の結果から、code/URL の範囲内だけにある finding を除外する。

最初からコードも mask して textlint に渡す実装へ置き換える場合、同じ結果になると仮定せず §15 の結果互換性を確認する。

`include_code` は Unihan/CJ option とし、textlint は Phase 2 では prose-only のまま。

---

## 29. Result merge

```text
選択された core 検査を実行
選択されていれば textlint を実行

要求した検査が1つでも実行不能 -> 非200（部分結果は返さない）
すべて正常終了 -> 選択された検査の issues を結合

warnings_as_errors を反映
stable sort
summary recompute
pass recompute
```

severity の二重変換を避ける。

現行 Go の `report.Normalize` に合わせ、結合後の結果で warning の昇格と安定ソートを行う。
順序は code-point の `start` 昇順、同じ位置なら error を先にし、それ以外の同順位では結合前の順序を維持する。
結合順は選択された Unihan、CJ、textlint の順にする。
core 内ですでに正規化される場合も、結合後の結果が local と一致することを確認する。
`summary` と `pass` は最終的な指摘一覧から算出する。

---

## 30. Performance gate

textlint 追加後に §17 の実環境検証を繰り返す。
dry-run は非圧縮サイズの確認であり、初回リクエストや実行時メモリの証拠にはしない。

```bash
wrangler deploy --dry-run
```

mandatory:

```text
size     < 64 MiB
startup  < 1 sec
memory   < 128 MB
```

推奨 safety margin:

```text
size     < 48 MiB
memory   peak < 96 MB
```

必須制限を満たせない場合は Phase 2 を停止し、配置先を再判断する。
Service Binding 分割は候補にとどめ、自動的に実施しない。

Cloudflare Service Binding:

- https://developers.cloudflare.com/workers/runtime-apis/bindings/service-bindings/
- https://developers.cloudflare.com/workers/runtime-apis/bindings/service-bindings/rpc/

---

## 31. Phase 2 tests

unit:

```text
textlint -> Issue
severity
UTF-16 -> code-point
Markdown/code exclusion
URL exclusion
warnings-as-errors
sort
summary
```

integration fixtures:

```text
正常な技術日本語
冗長表現
AI-writing pattern
長すぎる文
code block 内だけ
inline code 内だけ
emoji before finding
```

local textlint-enabled CLI と Workers API を同じ preset の版と設定で実行し、§15 の範囲で結果互換性を確認する。

境界と異常系:

- core 成功後の textlint 実行不能は非200で、部分結果を返さない。
- 警告だけの結果は合格、warnings-as-errors を有効にすると不合格。
- API モードのローカル専用設定は使われず、一度だけ無視を通知する。
- 修正ループ中に基準の版が変わっても、修正上限をリセットしない。

---

## 32. OMP / Pi Phase 2

既存 env を remote API request に再利用する。

```bash
export JPQG_BACKEND=api
export JPQG_API_URL=https://...
export JPQG_API_TOKEN=...
export JPQG_TEXTLINT=1
export JPQG_WARNINGS_AS_ERRORS=1
```

`JPQG_TEXTLINT=1` を:

```json
checks.textlint=true
```

に変換する。

correction loop の既定回数と停止動作は §16 のとおり維持する。
以下は修正によって合格する例であり、すべての応答で合格を保証するものではない。
警告だけなら修正せず、上限に達した不合格や途中の検査不能は通知して応答を返す。

```text
assistant
 -> API
 -> textlint finding
 -> warnings-as-errors
 -> correction
 -> API re-check
 -> pass
```

---

## 33. Phase 2 acceptance criteria

- [ ] `checks.textlint=true`
- [ ] subprocess 不使用
- [ ] `@textlint/kernel`
- [ ] Markdown processor
- [ ] current local preset と同等設定
- [ ] technical-writing findings
- [ ] AI-writing findings
- [ ] existing `issues[]` に統合
- [ ] default severity warning
- [ ] warnings_as_errors
- [ ] UTF-16/code-point位置変換
- [ ] code false positive 抑制
- [ ] URL false positive 抑制
- [ ] local CLI parity
- [ ] Worker < 64 MiB
- [ ] startup < 1 sec
- [ ] memory < 128 MB
- [ ] OMP remote + textlint correction
- [ ] Pi remote + textlint correction
- [ ] textlint 実行不能時に部分結果を返さない
- [ ] 警告だけなら合格し自動修正しない
- [ ] API 固定設定を適用し、ローカル専用指定の無視を一度通知
- [ ] 修正で合格する例と、上限で不合格のまま停止する例を確認
- [ ] textlint 辞書を使う lint を含め、§17 の実環境検証を再実施

---

# 34. Phase 3 は別計画

Phase 1〜2 では以下を含めない。

```text
natural-japanese
SudachiPy
Sudachi dictionary
lint.py
semantic.py
outline.py
terms.py
```

理由:

- subprocess 前提を避けたい
- Sudachi dictionary のサイズ/メモリ影響が大きい
- 128 MB isolate 制限と分けて評価したい

---

# 35. 推奨PR分割

## PR 1

```text
feat: add Cloudflare Workers core API
```

- Worker project
- Go/Wasm bridge
- `/v1/check`
- auth
- conformance

## PR 2

```text
feat: add remote API backend to OMP and Pi
```

- `JPQG_BACKEND`
- API transport
- timeout
- fail-open
- tests

## PR 3

```text
feat: add in-process textlint to Workers API
```

- `@textlint/kernel`
- bundled rules
- issue normalization
- parity tests

---

# 36. 実装順

```text
Architecture spike (不成立なら停止して配置先を再判断)
  |
  v
Go core Wasm bridge
  |
  v
/v1/check
  |
  v
Auth / size guard / error schema
  |
  v
local Go vs Worker conformance
  |
  v
OMP/Pi remote backend
  |
  +---- Phase 1 complete (§17 の検証後、単独公開可)
  |
  v
@textlint/kernel spike (不成立なら Phase 2 を停止して配置先を再判断)
  |
  v
Bundled rules
  |
  v
Issue normalization
  |
  v
Unicode/Markdown parity
  |
  v
local textlint vs Worker parity
  |
  v
OMP/Pi correction verification
  |
  +---- Phase 2 complete (§17 の検証を textlint 込みで再実施後)
```

---

# 37. Definition of Done

local:

```bash
printf '%s' 'これは经済です。' | jp-quality-gate
```

remote:

```bash
curl   -H "Authorization: Bearer $JPQG_API_TOKEN"   -H "Content-Type: application/json"   -d '{
    "text":"これは经済です。",
    "checks":{"unihan":true,"cj":true,"textlint":true}
  }'   https://<worker>/v1/check
```

この local コマンドは core のみ、remote コマンドは Phase 2 の全検査を要求するため、直接比較できるのは core の指摘である。
Phase 1 の remote 確認では `checks.textlint=false` にする。
Phase 2 の結果全体を比較するときは local でも §2 と同じ版の textlint と設定を有効にし、同じオプションで実行する。
合否、集計、説明文と順序を含む指摘全体を §15 に従って比較する。

OMP/Pi:

```bash
export JPQG_BACKEND=api
export JPQG_API_URL=https://<worker>
export JPQG_API_TOKEN=...
export JPQG_TEXTLINT=1
export JPQG_WARNINGS_AS_ERRORS=1
```

で:

```text
LLM response
 -> Workers API
 -> Unihan/CJ/textlint
 -> finding
 -> auto correction
 -> re-check
 -> pass
```

という修正で合格する例を確認する。
加えて、次の終了動作を実際の OMP/Pi で確認する。

- 修正上限に到達しても不合格なら、その状態を通知して最後の応答を返す。
- 検査不能なら通知して現在の応答を返し、通信再試行や local 再実行を行わない。
- 警告だけで warnings-as-errors が無効なら、合格として修正せず終了する。

「必ず合格する」は完了条件にしない。
各 Phase の acceptance criteria と §17 の実環境検証を満たし、測定できない必須項目がないことを公開条件とする。

---

# 38. 主なリスク

## Go/Wasm size/startup

対策:

- Phase 0 spike
- lazy init
- global-scope startup と遅延初期化を含む初回応答を分離して測定
- 不成立なら停止し、配置先を再判断（自動移植なし）

## CJ model memory

対策:

- isolate reuse
- requestごとにparseしない
- 1 KiB / 10 KiB / 100 KiB / 256 KiB benchmark
- 同時実行を含む isolate 全体のメモリ検証

## textlint Node依存

対策:

- `@textlint/kernel`
- filesystem config loader 不使用
- static imports
- 同じルール群を維持する rule-level static import の成立性確認
- 辞書を使う実際の lint を検証し、不成立なら Phase 2 を停止

## Unicode offset drift

対策:

- emoji/surrogate fixtures
- local Go conformance

## remote latency

用途を分ける。

```text
local = 最速 / オフライン
api   = centralized config / 複数端末
```

local CLI は残す。

---

# 39. 参考資料

Cloudflare Workers:

- https://developers.cloudflare.com/workers/platform/limits/
- https://developers.cloudflare.com/workers/languages/
- https://developers.cloudflare.com/workers/runtime-apis/webassembly/
- https://developers.cloudflare.com/workers/runtime-apis/webassembly/javascript/
- https://developers.cloudflare.com/workers/runtime-apis/bindings/service-bindings/
- https://developers.cloudflare.com/workers/observability/

textlint:

- https://github.com/textlint/textlint
- https://github.com/textlint/textlint/blob/master/docs/plugin.md
- https://github.com/textlint-ja/textlint-rule-preset-ja-technical-writing
- https://github.com/textlint-ja/textlint-rule-preset-ai-writing

jp-quality-gate:

- https://github.com/ktutumi/jp-quality-gate

---

# 40. 設計合意の対応表

ヒアリングで合意した17項目を、実装時に参照する節へ対応づける。
合意済みであることは、実装や検証の完了を意味しない。

| 質問 | 合意 | 反映先 |
| --- | --- | --- |
| Q1 | 同じ品質基準で結果を一致させ、基準の版と API 形式の版を区別 | §1、§7、§15 |
| Q2 | 自分が管理する端末向けの個人用 API に限定 | §0、§9 |
| Q3 | API モードの明示選択を全文送信への同意とし、送信禁止用途は local | §0、§14 |
| Q4 | 検査不能は合格と区別して fail-open。自動 local 再実行なし | §8、§16 |
| Q5 | Go/Wasm 不成立なら停止して配置先を再判断。自動移植なし | Phase 0、§4、§38 |
| Q6 | 説明文と順序を含む指摘全体を比較し、runtime 固有情報を除外 | §15、§29 |
| Q7 | 全検査無効と未対応検査の指定は400 | §6、§12、§19 |
| Q8 | API 固定設定を優先し、ローカル専用設定の無視を一度通知 | §23、§31、§33 |
| Q9 | 警告だけなら合格で自動修正しない | §26、§31、§37 |
| Q10 | 本文取得まで既定10秒、通信再試行なし、修正既定2回 | §16、§19 |
| Q11 | Phase 1 は単独公開可能。Phase 2 不成立時にルールを削減しない | §0、§22、§30、§36 |
| Q12 | 厳密な月額停止保証なし。利用量監視と異常時の token 失効で運用 | §9、§19 |
| Q13 | 要求した検査の部分失敗は要求全体の検査不能 | §7、§29、§31 |
| Q14 | 256 KiB超過は413、分割と切り詰めなし、空文字列は有効 | §6、§10、§17 |
| Q15 | 各検査で現行基準を使用し、更新時も修正回数をリセットしない | §7、§16、§31 |
| Q16 | 修正上限で不合格を通知して最後の応答を返す | §16、§32、§37 |
| Q17 | 実際の検証用 Worker で確認し、測れない必須項目を合格にしない | Phase 0、§3、§17、§30、§37 |
