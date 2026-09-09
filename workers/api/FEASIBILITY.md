# Issue #9 の成立性記録

> 作成日時: 2026-09-09 21:12
> 更新日時: 2026-09-10 01:15

## 判定

**証拠不足。STOP。**

ユーザーがデプロイした改修版 `820f287a-573d-4b9c-925d-5021640dd2eb` の成功ログから、非圧縮 bundle `13,076.83 KiB` と global startup `35 ms` を確認した。
改修版と native CLI の結果互換性を追加で18/18件確認し、認証・不正入力13件、サイズ・境界・並列要求の3系列39件も期待した status だった。
既存の改修版 live tail 記録13件は測定ID・CPU・wall time・versionの対応が揃っており、warm 1 KiB の CPU は3 msだった。
認証更新後の最新系列でも13件のHTTP要求と live tail を照合し、CPU時間を取得できた。
途中の認証エラーとHTTPのみの測定は履歴として保持し、最新系列と分けて記録した。

JavaScript と Wasm を含むピーク時の isolate メモリは未取得である。
cold と warm は区別できたが、cold でクライアント経過時間が10秒を超える要求が再現した。
実 Worker の4並列要求は異なる isolate に届いたため、同一 isolate の同時処理の証拠にはしない。
必須条件の証拠が揃っていないため、Issue #9 は未解決とし、後続の Issue #10 も開始しない。

## 必須条件に向けた改善（デプロイ確認済み）

Worker の Go runtime に `GOMEMLIMIT=48MiB` を設定した。
[Go の仕様](https://pkg.go.dev/runtime#hdr-Environment_Variables)にある GC の soft limit であり、JavaScript、Wasm の全領域、isolate 全体の hard limit ではない。
初期化時のモデル解析と一時割当てによる Wasm 領域の拡大を抑えるために使用する。
既存 core、モデル、辞書、local CLI、標準 runtime glue は変更していない。

認証済みの成功応答の `meta` に次の情報を追加した。
本文と token はログに出さない。

| フィールド | 意味 |
| --- | --- |
| `validation_isolate_id` | 初回の有効要求で生成し、その isolate で再利用するランダム UUID |
| `validation_request_sequence` | core に進んだ要求の isolate 内連番 |
| `validation_initialization` | `cold`: 初期化を開始、`waiting`: 初期化中の Promise を共有、`warm`: 初期化完了後 |

初期化状態は `getCore()` を await する前に確定する。
要求が並列送信されたことと、同一 isolate に届いたことを別々に確認できる。
8 件の既存 HTTP テストが pass し、そのうち cold 同時要求のテストでは、異なる入力の結果互換性に加えて同じ ID、重複しない連番、cold が 1 件だけであること、続く要求が warm であることを検証した。
改善後の最終確認では `npm run build`、`npm run typecheck`、`npm test`（8/8）、`npm run measure`、`make check`（Go tests/vet、OMP 6/6、Pi 9/9）が pass した。

`npm run measure` に、fresh なローカル Worker、1/10/100/256 KiB、空文字、decoded 超過、Unicode escape、streaming raw body 超過、異なる 256 KiB の 4 並列を再現するスクリプトを追加した。
今回の13要求中、成功11件は同じ isolate ID で、最初の1件が cold、残り10件は warm だった。
超過2件は `413` で、core のメタデータを返さない。
CPU と全体の peak memory はこのスクリプトでは取得せず、明示的に `null` として記録する。
live tail と照合できるよう、各要求の `x-jpqg-measurement` にランダムな測定 ID を送り、`measurement_id` として保存する。
下記の新旧比較の取得後にこの相関ヘッダーを追加したため、比較記録に測定 ID は含まれない。

同じ計装済み bundle から `GOMEMLIMIT` の代入だけを除いた対照と比較した。
それぞれ fresh なローカル workerd で1回ずつ測定した値である。

| 観測値 | soft limit なし | `48MiB` 設定後 |
| --- | ---: | ---: |
| 初回 1 KiB の client 経過時間 | 1,133.99 ms | 1,102.71 ms |
| warm 1 KiB の client 経過時間 | 4.93 ms | 4.11 ms |
| warm 256 KiB の client 経過時間 | 38.65 ms | 42.54 ms |
| Wasm sampled capacity 最大 | 104,333,312 bytes | 83,886,080 bytes |

Wasm sampled capacity は約19.6%減った。
GC が増えるため CPU とのトレードオフがあり、応答時間の改善は主張しない。
今回の dry-run upload は `13,076.83 KiB`、gzip は `9,036.61 KiB` だった。
新版の実デプロイ時 startup は、今回確認した成功ログでは `35 ms` だった。
dry-run と実デプロイの bundle hash の同一性は未検証であり、実応答の計装フィールドと live tail の version ID を照合した。
正本の `improvement_measurements` に新旧の全測定値、観測時刻、bundle hash を保存した。
既存のローカル／実 Worker の測定値は履歴として保持する。

### 再デプロイ後の確認（2026-09-10 JST）

成功ログ `wrangler-2026-09-09_14-57-26_226.log` の version ID は `820f287a-573d-4b9c-925d-5021640dd2eb` だった。
実 upload は `13,076.83 KiB`、gzip は `9,037.68 KiB`、global startup は `35 ms` である。
既存の `npm run measure` を実 Worker に対して実行し、13/13 件が期待した status となった。
測定 ID で全13件の live tail と照合し、すべて同じデプロイ version、outcome `ok` だった。
成功11件には isolate ID・連番・初期化状態があり、超過2件は `413` でメタデータを返さなかった。

| ケース | 初期化 | client ms | CPU ms | Worker wall ms |
| --- | --- | ---: | ---: | ---: |
| `first-1KiB` | cold | 6019.24 | 4274 | 5386 |
| `reuse-1024` | warm | 205.69 | 3 | 4 |
| `reuse-10240` | warm | 211.02 | 8 | 12 |
| `reuse-102400` | warm | 631.32 | 55 | 57 |
| `reuse-262144` | warm | 702.31 | 123 | 129 |
| `empty` | warm | 198.96 | 1 | 1 |
| `decoded-overflow` | — | 401.85 | 5 | 15 |
| `escaped-256KiB` | cold | 11492.62 | 7769 | 9554 |
| `raw-overflow` | — | 3163.58 | 1 | 2 |
| `concurrent-0` | warm | 1848.58 | 272 | 280 |
| `concurrent-2` | cold | 6486.94 | 4265 | 5434 |
| `concurrent-3` | cold | 8962.34 | 6470 | 7572 |
| `concurrent-1` | cold | 10454.22 | 7083 | 9534 |

最初の 1 KiB は `cold`、続くサイズ別要求は同じ isolate の `warm`、連番は1〜6だった。
warm 1 KiB の CPU `3 ms` は、今回の観測では推奨の50 ms未満を満たす。
4 並列 256 KiB はすべて `200` で、各入力の簡体字の検出位置 `0, 1, 2, 3` も一致したが、4件は別々の isolate に届いた。
同一 isolate の同時処理や `waiting` 応答は今回観測していない。

cold は5件あり、Unicode escape の 256 KiB は client `11,492.62 ms`、並列要求の1件は `10,454.22 ms` だった。
対応する Worker wall time はそれぞれ `9,554 ms`、`9,534 ms` である。
通信・アップロードを含む client 時間と Worker wall time は区別し、初回要求10秒以内を全ケースで満たしたとは判定しない。
Wasm sampled capacity の最大は `89,653,248 bytes` で、isolate 全体のピークは未取得である。
正本は `feasibility.json` の `improvement_measurements.deployed_verification` に保存した。

### 追加の実 Worker 確認（2026-09-10 00:39 JST）

環境変数 `JPQG_API_TOKEN` を読み込み、既存 endpoint に再測定した。
再デプロイと secret の変更は行っていない。
3系列の13要求、計39件がすべて期待した status（各系列 `200` が11件、`413` が2件）を返した。
別の検証では、改修版と native CLI の `meta` を除く結果全体が18/18件一致し、認証3件は `401`、不正入力10件は `400` だった。
位置の異なる4入力の互換性確認は逐次送信で、並列の結果混入検査は13要求の測定系列に含む。

| 追加系列 | 最初の cold 1 KiB client ms | warm 1 KiB client ms | 4並列の最大 client ms |
| --- | ---: | ---: | ---: |
| 1 | 7,288.84 | 153.75 | 10,459.11（cold） |
| 2 | 5,324.97 | 194.03 | 10,239.21（cold） |
| 3 | 8,682.21 | 202.01 | 6,111.86（cold） |

今回も各系列の4並列は4個の異なる isolate ID を返した。
Wasm sampled capacity の最大は `91,750,400 bytes`（87.5 MiB）だった。
これは全体ピークの証拠ではない。
初回の小さい入力が10秒未満であることだけでは、並列 cold の10秒超過を解消したと判断しない。

今回の live tail は、保存済み OAuth の期限切れと、API token による対象 Worker の tails API の認証エラー `10000` により取得できなかった。
`CLOUDFLARE_API_KEY` の値が有効な API token であることは公式 verify API で確認し、子プロセス内だけで `CLOUDFLARE_API_TOKEN` として渡したが、対象操作へのアクセスは得られなかった。
Worker HTTP 用の `JPQG_API_TOKEN` は正常に動作している。
今回のCPU値を過去の別要求から補完せず、`null` のまま保存した。
既存の改修版CPU記録13件については、保存された測定ID・CPU/wall time・versionの整合を再確認した。

正本の `improvement_measurements.additional_deployed_verification` に、3系列の全数値、互換性・入力検査結果、今回の認証制約を追記した。
既存の `deployed_verification` は保持した。

### 初期化の割当て削減案の検証（不採用）

保存済みのローカル cold profile では、gzip 展開、CJ モデル行の解析、割当て・GC に実行サンプルが集まっていた。
モデルの値を変えずに各行の分割用 slice を再利用する案を、`strings.SplitSeq` と `strings.Cut` の2通りで試した。
ネイティブGoのモデル読込 benchmark では `SplitSeq` 案の割当てが約198万回から約99万回、約191 MBから約127 MBへ減ったが、Wasmでは悪化した。

| ローカル fresh Worker、各3回 | cold client ms の中央値 | cold 後の Wasm capacity 最大 bytes |
| --- | ---: | ---: |
| 元の `strings.Split` | 1,104.20 | 89,653,248 |
| slice 再利用 + `strings.SplitSeq` | 1,265.55 | 156,762,112 |
| slice 再利用 + `strings.Cut` | 1,306.31 | 156,762,112 |

両案とも Wasm capacity だけで128 MiBを超え、初回時間も悪化したため採用しなかった。
Goソースと試行用テストを元に戻し、生成済みWorkerも再ビルドした。
復元後の対照は cold `1,140.95 ms`、Wasm capacity `89,653,248 bytes` で、ビルド・型検査・HTTPテスト8/8もpassした。
この試行は実 Worker にデプロイしていない。
記録は `improvement_measurements.rejected_parser_trials` に保持し、ネイティブの割当て量を Worker の改善の証拠として扱わない。

### 認証更新後のCPU測定とメモリ集計（2026-09-10 01:15 JST）

更新された既存の Wrangler OAuth で live tail が接続でき、全13件を測定IDで照合した。
HTTPは期待する status を返し、trace はすべて outcome `ok`、version `820f287a-573d-4b9c-925d-5021640dd2eb` だった。
以前の認証エラーを現在の停止理由にはしない。

| ケース | 初期化 | client ms | CPU ms | Worker wall ms |
| --- | --- | ---: | ---: | ---: |
| 1 KiB 初回 | cold | 5,962.15 | 4,567 | 5,077 |
| 1 KiB 再利用 | warm | 288.39 | 3 | 4 |
| 10 KiB 再利用 | warm | 296.86 | 8 | 9 |
| 100 KiB 再利用 | warm | 888.22 | 55 | 57 |
| 256 KiB 再利用 | warm | 1,518.46 | 132 | 137 |
| Unicode escape 256 KiB | cold | 11,472.08 | 6,831 | 8,265 |
| 4並列中の最長要求 | cold | 26,058.03 | 5,712 | 6,245 |

今回の4並列も異なる4個の isolate に届いた。
client時間とWorker wall timeの差を、Go初期化の処理時間として計上しない。
差の内訳は未測定であり、ネットワークだけが原因とも断定しない。
今回もclient時間の10秒条件を全ケースで満たしたとは判断できない。

GraphQL の実スキーマには `AccountWorkersInvocationsAdaptiveMax.memoryUsageBytes` と `wasmMemoryBytes` があり、percentile以外の最大集計値も取得できた。
UTC `2026-09-09T14:10:50Z`〜`16:10:50Z` を対象に `scriptVersion` ごとに取得した値は次のとおり。

| version | `sum.requests` | `max.memoryUsageBytes` | `max.wasmMemoryBytes` |
| --- | ---: | ---: | ---: |
| 改修前 `e22d2446-…` | 64 | 111,039,248 bytes | 102,236,160 bytes |
| 改修版 `820f287a-…` | 114 | 99,435,263 bytes | 91,750,400 bytes |

改修版のV8 isolate memory最大集計値は約94.83 MiBで、128 MBを下回る観測値を得た。
これは上記時間窓のinvocation集計であり、今回の13要求と一対一に対応付けた値ではない。
[公式ドキュメント](https://developers.cloudflare.com/workers/observability/metrics-and-analytics/#memory-usage)はmemory usageをinvocation時点の共有isolateメモリとして説明している。
したがって `max` で集約しても、invocation間・実行途中を連続監視したピークや全割当ての保守的上界が得られたとは判定しない。
`memoryUsageBytes` と `wasmMemoryBytes` の包含関係を推測して加算することもしない。
全体ピークの必須証拠は引き続き未取得である。

このCPU測定の直前の試行は `reuse-102400` でclientの30秒タイムアウトが発生した。
旧スクリプトは途中結果を出力しなかったため、その試行のCPU値や成功分を推測して補わない。
測定スクリプトを修正し、途中失敗でも `completed: false` と取得済み記録・通信失敗情報をJSONで出力し、非ゼロ終了するようにした。
並列要求はすべての成否が確定してから出力し、失敗したHTTP statusも検証前に保存する。
ローカルの接続失敗による回帰テストを追加し、通常の測定13件と合わせて確認した。

新しい正本は `improvement_measurements.reauthenticated_deployed_verification` と `graphql_memory_observation` に保存した。
認証情報、本文、未加工のtailヘッダーは保存していない。

### 残る必須確認

1. JavaScript と Wasm を含む isolate 全体のピーク値、またはその全体を覆う保守的な上界を取得する。
2. cold の10秒条件について、上記の超過ケースと計測範囲を確認する。
3. 同一 isolate の同時要求を実 Worker で観測する。今回の4並列成功はその証拠にしない。

[Cloudflare の memory usage メトリクス](https://developers.cloudflare.com/workers/observability/metrics-and-analytics/)は invocation 時の reservoir sampling による percentile である。
`P999`、DevTools の heap snapshot、今回の Wasm capacity のいずれも全体ピークの証明には置き換えない。
公開メトリクスだけでは必須のピーク条件を証明できる方法が確認できていないため、運用者／Cloudflare 側で取得方法を確認する必要がある。
この条件の充足までは「証拠不足」と Issue #10 の停止を維持する。

Node.js 互換 API も代替のピーク測定器にはならない。
確認した [workerd の公開実装](https://github.com/cloudflare/workerd/blob/22c1ca05709f24134b4c95bab6cb76d4dde38b32/src/node/internal/public_process.ts#L284-L335)では、`process.memoryUsage()` の全フィールドと `process.resourceUsage()` の `maxRSS` を含む全フィールドが固定の `0` を返す。
この `0` を実測値として採用せず、測定のためだけに `nodejs_compat` を追加しない。
これは公開ソースの確認結果であり、実 Worker のピークメモリを観測した結果ではない。

以下の旧測定・旧判定は履歴として保持する。最新の確認結果と残項目は上記の再デプロイ後の確認を参照する。

## 観測範囲

| 項目 | 記録 |
| --- | --- |
| 実行範囲 | ローカル Miniflare/workerd と実 Worker の HTTP、live tail |
| 実 Worker | https://jp-quality-gate-validation.ktutumi.workers.dev/ |
| Go | `1.27.1` |
| Wrangler | `4.130.0` |
| Miniflare | `5.20260908.0-alpha` |
| TypeScript | `7.0.2` |
| Node.js | `26.8.1` |
| 最新の Wrangler dry-run の bundle 合計 | `13,076.83 KiB` |
| 記録の正本 | [`feasibility.json`](./feasibility.json) |

`feasibility.json` はローカル測定を保持し、`deployed_measurements` に実 Worker の HTTP 検証と live tail の記録を追加したファイルである。
Go など上表のツールチェーンはローカル測定時の値である。
`bundle_total_upload_kib` は、今回の改善前の dry-run 値 `13076.41` を保持している。
今回の dry-run 値は上記の改善記録に記載した。
profile と heap の観測は、修正前に取得した通常経路の記録を保持している。
`.generated/`、`dist/`、`.wrangler/` と生成された CPU profile はローカル生成物として無視する。

ビルドは標準 Go の `js/wasm` と runtime glue を使う。
`wasm_exec.js` はインストール済み Go の `GOROOT` から変更せずにコピーし、同じ場所へ Go の `LICENSE` をコピーする。
local CLI、WASI、別言語への移植、prose subprocess、設定ファイル読込経路はこの検証経路に含めない。

## ローカルで再現した経路

`workers/api/README.md` に、次のコマンドを含む再現手順を置いた。

```sh
npm ci
npm run build
npm run typecheck
npm test
```

`npm run build` は Go/Wasm のビルドと Wrangler の dry-run bundling を行うが、デプロイは行わない。
親セッションの最終確認では、clean な環境の `make check`（Go tests と vet、OMP 6/6、Pi 9/9）、Wasm `go vet`、`npm run build` 2 回、`npm run typecheck`、`npm test` 8/8 が pass した。
これらはローカルの確認であり、デプロイ後の Worker の挙動を示さない。

HTTP endpoint は `POST /v1/check` である。
認証には `Authorization: Bearer <token>` を使い、ローカルではプロセス環境変数から使い捨て token を渡す。
検証用 secret と production の設定を混ぜないため、Wrangler の操作には常に `--config wrangler.validation.jsonc` を指定する。

本文は次の JSON 形だけを受け付ける。

```json
{
  "text": "検査対象",
  "options": {
    "cj_min_cjk": 4,
    "cj_min_gap": 0.15,
    "include_code": false,
    "warnings_as_errors": false
  }
}
```

`text` は必須の文字列である。
`options` は省略でき、4 個の既知キーだけを指定できる。
Issue #9 では `checks` 選択子を提供しないため、`checks` を含む要求は `400` になる。

## 本文制限と測定方法

実装が検査する上限は次の二つである。

| 対象 | 上限 | 判定方法 |
| --- | ---: | --- |
| 生の HTTP body | `6 * 256 KiB + 4096 = 1,576,960` bytes | `Content-Length` の早期検査と streaming reader の累積バイト数 |
| JSON の `text` | `256 KiB = 262,144` bytes | UTF-8 を strict decode した後の `TextEncoder` バイト数 |

上限を超えた要求は切り詰めず、分割せず、core に渡さずに `413` を返す。
追加のローカル fault-injection smoke では、raw-body guard を有効にした場合に `413`、guard を無効にした対照では `200` になった。
この対照により、`413` が固定応答ではなく実際の上限 guard から返ることを確認した。
測定ではローカル HTTP endpoint に要求を送り、クライアント側の経過時間、HTTP status、GateResult の summary を記録した。
Profiler 使用時は DevTools の profile interval と non-idle sample、要求前後の V8 heap sample、Wasm linear-memory capacity を別々に採取した。

`local_profile_nonidle_sample_ms` はローカル profiler のサンプル値であり、Cloudflare の billed CPU ではない。
値が `0` でも、サンプリング間隔より短い処理の仕事量が `0` とは限らない。

## 通常要求の結果

未プロファイルの HTTP 測定は次の結果になった。
初回要求 `cold` の `text` は `これは经済に関する説明です。` で、UTF-8 で 42 bytes だった。

| ケース | 入力バイト数 | status | 経過時間 ms | summary | Wasm linear memory bytes |
| --- | ---: | ---: | ---: | --- | ---: |
| 初回要求 `cold` | 42 | 200 | `1089.48` | errors 1, warnings 0, issues 1 | `90,701,824` |
| warm | 1,024 | 200 | `4.66` | 0, 0, 0 | `90,701,824` |
| warm | 10,240 | 200 | `5.39` | 0, 0, 0 | `90,701,824` |
| warm | 102,400 | 200 | `21.20` | 0, 0, 0 | `90,701,824` |
| warm | 262,144 | 200 | `37.34` | 0, 0, 0 | `90,701,824` |
| 空の `text`（JSON body） | 0 | 200 | `1.17` | 0, 0, 0 | 記録なし |
| 上限超過 | 上限超過 | 413 | `1.79` | なし | 記録なし |

表の経過時間は読みやすさのため小数第 2 位に丸め、完全な精度は `feasibility.json` に保持した。
初回要求の経過時間はローカルで 10 秒以内だったが、Cloudflare の global startup の証拠ではない。

core の error envelope を注入するローカル HTTP smoke では、修正前の `200` から修正後の generic `500` へ変わった。
この修正は error 経路だけに及び、通常経路の CPU と heap profile は再測定していない。

## プロファイル付きの結果

Profiler で記録した要求は次の通りである。

| ラベル | 同時数 | 入力 bytes | status | 経過時間 ms | non-idle sample ms | profile interval ms |
| --- | ---: | ---: | --- | ---: | ---: | ---: |
| `cold-1KiB` | 1 | 1,024 | 200 | `1090.52` | `1086.91` | `1092.34` |
| `warm-1024` | 1 | 1,024 | 200 | `3.50` | `0.00` | `5.37` |
| `warm-10240` | 1 | 10,240 | 200 | `4.93` | `4.12` | `6.71` |
| `warm-102400` | 1 | 102,400 | 200 | `17.70` | `16.62` | `19.54` |
| `warm-262144` | 1 | 262,144 | 200 | `36.34` | `35.60` | `38.23` |
| `concurrent-4x256KiB` | 4 | 262,144 × 4 | 200 × 4 | `155.63` | `153.98` | `157.60` |

`concurrent-4x256KiB` は 4 件すべてが `200` になった。
各入力には異なる位置に簡体字を 1 文字含めた。
4 件すべての summary は errors 1、warnings 0、issues 1 で、指摘の開始位置は入力に対応する `0, 1, 2, 3` だった。
この結果から同時要求が同一 isolate で動いたとは判断しない。

プロファイル付き要求で観測した Wasm linear-memory capacity の最大値は `104,595,456` bytes だった。
これは各 Wasm instance の `memory.buffer.byteLength` から得た exact sampled capacity であり、Go の live heap の領域を含みうる。
isolate 全体のメモリに対しては下限情報だが、Wasm 自体の下限とみなしてはならない。
JavaScript heap、embedder、runtime、その他の割当てを含む isolate 全体の peak memory ではないため、Workers の 128 MB 条件を満たしたとは判定しない。

### Heap sample は別の観測値

V8 の heap sample は Wasm linear memory と別に保存した。
次の表は `usedSize` の要求前後と、その時点の `totalSize`、`backingStorageSize` である。

| ラベル | usedSize before → after | totalSize before → after | backingStorageSize before → after |
| --- | ---: | ---: | ---: |
| `cold-1KiB` | `1594596 → 1630804` | `2883584 → 2883584` | `737884 → 737884` |
| `warm-1024` | `1630804 → 1644336` | `2883584 → 2883584` | `737884 → 749309` |
| `warm-10240` | `1644336 → 1692864` | `2883584 → 2883584` | `749309 → 797004` |
| `warm-102400` | `1692864 → 1757308` | `2883584 → 4194304` | `797004 → 737884` |
| `warm-262144` | `1757308 → 2628128` | `4194304 → 4980736` | `737884 → 1811055` |
| `concurrent-4x256KiB` | `2628128 → 4214744` | `4980736 → 6881280` | `1811055 → 2609794` |

これらは要求前後のサンプルであり、isolate 全体のピーク値ではない。

## 実 Worker の結果（改善前の履歴）

対象は `https://jp-quality-gate-validation.ktutumi.workers.dev/v1/check` である。
ユーザーのデプロイ成功報告を受けて測定し、再デプロイや secret の変更は行っていない。
既存の成功ログ `wrangler-2026-09-09_14-05-36_184.log` には、非圧縮 upload `13,076.41 KiB`、gzip `9,037.53 KiB`、`Worker Startup Time: 16 ms` が記録されていた。

実 HTTP と native CLI の `pass`、`summary`、メッセージを含む順序付き `issues` は 18/18 件で一致した。
runtime の `meta` は比較から除いた。
日本語、簡体字、補助平面文字、Markdown、URL、コード領域、warning の error 昇格を含む。
認証 3 件は `401`、不正 JSON、UTF-8、schema、option など 11 件は `400` だった。
空の text、厳密な 256 KiB、最悪ケースの Unicode escape は `200`、decoded text 超過と streaming raw body 超過は `413` だった。

HTTP 検証系列のサイズ別応答時間は次のとおりである。

| 入力 bytes | status | client 経過時間 ms |
| --- | ---: | ---: |
| 1,024 | 200 | 144.53 |
| 10,240 | 200 | 149.96 |
| 102,400 | 200 | 328.48 |
| 262,144 | 200 | 508.59 |

最初に観測した 42 bytes の要求は `5,189.43 ms` だった。
4 並列の 256 KiB はすべて `200` で、各要求は約 `591.72 / 818.36 / 698.76 / 5,819.33 ms` だった。
`batch_cli_and_http_elapsed_ms = 6142.559` は native CLI との比較処理も含むため、純粋な HTTP batch latency としては扱わない。
この系列の Wasm 線形メモリ容量は `94,896,128` bytes だった。

### Live tail の CPU 時間

[Cloudflare の実行時間フィールド](https://developers.cloudflare.com/changelog/post/2025-04-09-workers-timing/)を、[Wrangler の live tail](https://developers.cloudflare.com/workers/observability/logs/real-time-logs/)から取得した。
別系列の 6 要求に安全な測定用ヘッダーを付け、順序付き trace と対応付けた。
保存したのは CPU 時間、wall time、測定ラベルなどだけで、認証ヘッダーや token は保存していない。
すべて HTTP `200`、trace の outcome は `ok` だった。

| 入力 bytes | Worker CPU ms | Worker wall ms | client 経過時間 ms |
| --- | ---: | ---: | ---: |
| 1,024 | 6,001 | 7,257 | 7946.60 |
| 10,240 | 5,725 | 6,541 | 7123.89 |
| 102,400 | 75 | 78 | 1118.47 |
| 262,144 | 125 | 131 | 983.94 |
| 1,024（再要求） | 4,239 | 5,376 | 5934.28 |
| 10,240（再要求） | 4,091 | 5,396 | 5893.17 |

小さい入力でも CPU が 4〜6 秒の要求を観測した。
この系列を cold と warm に分類する根拠は取得しておらず、推奨条件「warm 1 KiB の CPU 50 ms 未満」を満たしたとは判定しない。
別系列の HTTP 時間とこの CPU 時間を同じ要求の値として組み合わせることもできない。

実 Worker で観測した Wasm 線形メモリ容量の最大値は `102,236,160` bytes だった。
HTTP が成功したことや、この容量が 128 MB を下回ったことから、isolate 全体のピークが 128 MB 未満だったとは断定できない。

## 受け入れ基準との照合（改善前の履歴）

| 基準 | 現時点の証拠 | 判定 |
| --- | --- | --- |
| HTTP、JSON、既存 Go core、GateResult | 実 Worker と native CLI の結果が 18/18 件一致 | 観測範囲で確認 |
| Bearer、本文境界、不正入力 | 実 HTTP で期待 status を確認 | 観測範囲で確認 |
| 異なる同時要求 | 4 並列 256 KiB がすべて 200、local parity も一致 | 同一 isolate の保証ではない |
| 非圧縮 bundle 64 MiB 未満 | 新版 dry-run `13,076.83 KiB`、既存デプロイ `13,076.41 KiB` | 適合 |
| global startup 1,000 ms 未満 | 既存デプロイの `Worker Startup Time: 16 ms` | 既存 build は適合、新版は未測定 |
| JavaScript と Wasm を含む isolate memory 128 MB 未満 | sampled Wasm capacity 最大 `102,236,160` bytes のみ | 全体のピークは未取得 |
| 初回要求 10 秒以内 | 初回観測 `5,189.43 ms`、追加測定の最大 `7,946.60 ms` | cold への帰属は未確認 |
| CPU 時間 | live tail の対応付き 6 件を取得 | 実測値あり |
| 推奨 bundle 32 MiB、startup 500 ms、warm 1 KiB CPU 50 ms | bundle と startup は下回る。warm CPU は未確認 | 必須条件と区別 |

## 残る証拠と過去の失敗（改善前の履歴）

JavaScript と Wasm を含む isolate 全体のピークメモリが残っている。
V8 の要求時点のサンプルや quantile は、そのままピークの証明には使わない。
cold と warm の帰属も未確認なので、初回要求と warm CPU の条件には留保がある。
追加測定の前に、これらを観測できる方法を決める必要がある。

以前の OAuth 期限切れと account API の `403` は、デプロイ前の試行履歴である。
今回はユーザーのデプロイが成功し、既存ログと live tail も利用できたため、過去の `403` を現在の停止理由にはしない。
ローカルの `wrangler check startup` で発生した static-files-directory detection と引数解析の失敗は `feasibility.json` に保持した。
現在の startup 判定には、その失敗ではなく成功したデプロイの `16 ms` を使う。

不足する必須測定が揃うまで Issue #9 を成立扱いにせず、Issue #10 の停止を維持する。
今回の計装と GC 設定を反映する再デプロイ後も、未取得のピークメモリを推測で埋めない。

## 用語

**初回要求**：新しい local worker 実行へ最初に送る要求で、遅延初期化を含む。

**warm 要求**：初期化後に送る要求で、初回初期化の時間を分けて観測する。

**経過時間**：HTTP client が要求を送ってから応答を受け取るまでの wall-clock 時間である。

**non-idle sample**：local profiler がサンプリングした実行中の非 idle 区間であり、Cloudflare の課金 CPU 時間ではない。

**Wasm linear memory**：`memory.buffer.byteLength` から得た Wasm instance の exact sampled capacity であり、Go live heap の領域を含みうるが、Worker isolate 全体のメモリではない。

**heap sample**：要求前後に採取した V8 heap の観測値であり、peak isolate memory ではない。

**観測範囲**：測定した実行環境、入力、時刻、測定器が結果に含める範囲である。

**証拠不足**：成立または不成立を断定するために必要な測定が未取得の状態である。
