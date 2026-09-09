# Issue #9 の成立性記録

> 作成日時: 2026-09-09 21:12
> 更新日時: 2026-09-09 21:24

## 判定

**証拠不足。STOP。**

今回の測定はすべてローカルの Miniflare/workerd で行った。
Cloudflare 上の実 Worker を測定できなかったため、Issue #9 は未解決のままにする。
後続の Issue #10 も開始しない。

ローカルの HTTP 経路が動いたことは、デプロイ後の成立を意味しない。
同時要求が同一 isolate で処理されたこと、Cloudflare の CPU 時間、global startup、ピーク時の isolate メモリも確認できていない。

## 観測範囲

| 項目 | 記録 |
| --- | --- |
| 実行範囲 | ローカル Miniflare/workerd のみ |
| Go | `1.27.1` |
| Wrangler | `4.130.0` |
| Miniflare | `5.20260908.0-alpha` |
| TypeScript | `7.0.2` |
| Node.js | `26.8.1` |
| 最新の Wrangler dry-run の bundle 合計 | `13,076.41 KiB` |
| 記録の正本 | [`feasibility.json`](./feasibility.json) |

`feasibility.json` は、最新の `.generated/measurements.json` を数値を変えずに保存したファイルである。
`bundle_total_upload_kib` は、エラー経路修正後の最新 dry-run 値 `13076.41` を記録している。
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

## 受け入れ基準との照合

| 基準 | 現時点の証拠 | 判定 |
| --- | --- | --- |
| 標準 Go/Wasm から HTTP、JSON、既存 Unihan/CJ、GateResult までの経路 | ローカル endpoint で動作を観測 | デプロイ後は未確認 |
| 遅延初期化と異なる同時要求 | ローカル試行で 4 件の status は 200 | 同一 isolate の証明なし |
| Bearer、本文上限、token と本文の非ログ化 | ローカルコードと HTTP 試験で確認 | デプロイ後は未確認 |
| local と CLI の結果互換性 | `npm test` 8/8、clean 環境の `make check`、Wasm `go vet` が pass | デプロイ後は未確認 |
| 非圧縮 bundle 64 MiB 未満 | 最新 local dry-run の合計 `13,076.41 KiB` | デプロイ upload の確認なし |
| global startup 1,000 ms 未満 | `startup_time_ms` がない | 証拠不足 |
| JavaScript と Wasm を含む isolate memory 128 MB 未満 | exact sampled linear capacity 最大 `104,595,456` bytes と heap sample のみ | 証拠不足 |
| 初回要求 10 秒以内 | local 経過時間 `1089.48 ms` | デプロイ後は未確認 |
| CPU 時間 | local non-idle sample のみ | Cloudflare billed CPU は未測定 |
| 推奨 bundle 32 MiB、startup 500 ms、warm 1 KiB CPU 50 ms | local bundle は下回るが、他の値は条件に対応する証拠なし | 必須条件と混同しない |

## 未取得の測定と失敗した試行

既定の Wrangler OAuth token は有効期限が切れ、refresh も失敗した。
別に利用可能な credential は verify で active だったが、設定された validation account の Workers API にはアクセスできず、`403 Authentication error` になった。
secret の値、account identifier、credential の内容は記録しない。
そのため、実 Worker の upload、secret 設定、実 URL への要求を実行できなかった。

その結果、次の項目は未取得である。

- deployed startup と `startup_time_ms`
- Cloudflare billed CPU time
- JavaScript と Wasm を含む peak isolate memory
- 実 Worker での local parity
- 実 Worker の同時要求が同一 isolate に配置されたこと

startup profiling では次の二つを試したが、どちらも `startup_time_ms` を生成しなかった。

```text
wrangler check startup --config wrangler.validation.jsonc
  static-files-directory detection error

wrangler check startup --args "--config wrangler.validation.jsonc"
  CLI argument parsing error
```

この二つは失敗した試行の記録であり、動作する再現手順として推奨しない。
デプロイ後の startup は認証済みの upload と実 Worker の測定で確認する必要がある。

## 再開条件

1. validation account の Workers API にアクセスできる credential を用意し、production の設定と secret を使わない。
2. `wrangler secret put JPQG_API_TOKEN --config wrangler.validation.jsonc` で検証用 secret を設定する。
3. `wrangler deploy --config wrangler.validation.jsonc` の結果と実 URL を記録する。
4. 実 Worker で startup、billed CPU、peak isolate memory、本文境界、初回、warm、4 並列、local parity を再測定する。
5. 同時要求の isolate 配置は観測範囲として記録し、同一 isolate で処理されたという保証を主張しない。
6. 必須測定が揃うまで Issue #9 を成立扱いにせず、Issue #10 の停止を維持する。

## 用語

**初回要求**：新しい local worker 実行へ最初に送る要求で、遅延初期化を含む。

**warm 要求**：初期化後に送る要求で、初回初期化の時間を分けて観測する。

**経過時間**：HTTP client が要求を送ってから応答を受け取るまでの wall-clock 時間である。

**non-idle sample**：local profiler がサンプリングした実行中の非 idle 区間であり、Cloudflare の課金 CPU 時間ではない。

**Wasm linear memory**：`memory.buffer.byteLength` から得た Wasm instance の exact sampled capacity であり、Go live heap の領域を含みうるが、Worker isolate 全体のメモリではない。

**heap sample**：要求前後に採取した V8 heap の観測値であり、peak isolate memory ではない。

**観測範囲**：測定した実行環境、入力、時刻、測定器が結果に含める範囲である。

**証拠不足**：成立または不成立を断定するために必要な測定が未取得の状態である。
