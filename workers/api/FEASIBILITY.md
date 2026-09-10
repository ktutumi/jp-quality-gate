# Issue #9 の成立性記録

> 作成日時: 2026-09-09 21:12
> 更新日時: 2026-09-10 15:14

## 判定

**証拠不足。STOP。**

packed版 `2efc5d35-f637-425c-901a-d9063abc100f` のデプロイと、配備JS/Wasmのhash一致を確認した。
非圧縮uploadは33,695.45KiB、global startupは20ms。
今回の200要求はすべて期待statusで、計測できたcold27件のclient時間は最大5,095.85msだった。
通常native CLIとの結果は、実行環境固有の診断meta4項目だけを除いて18/18件一致した。

HTTP動作とcold短縮を確認したが、全体ピークメモリ、cold 100KiB、同一isolateで重なった初期化要求は未確認。
CPU/wallは169計測要求中109件を照合でき、60件が欠落した。
このためIssue #9は証拠不足を維持し、Issue #10を開始しない。
最新の実測は次節、legacy版の10秒超過などの過去記録は後段に保持する。

## packed版のデプロイ後検証（2026-09-10 15:11 JST）

正本は `feasibility.json.packed_model_measurements.deployed_verification`。
検証開始・終了時のactive deploymentは同一version、配分は100%だった。
[Get script content API](https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/content/methods/get/)で配備moduleを読み、WasmとJSのbytes・SHA-256が `dist/packed/build-manifest.json` と一致することを確認した。
ダウンロードした本文や認証情報は記録せず、module名・サイズ・hashだけを保存した。

| 項目 | 結果 |
| --- | --- |
| version | `2efc5d35-f637-425c-901a-d9063abc100f` |
| deployment時刻 | 2026-09-10 05:55:15 UTC |
| 非圧縮upload | 33,695.45KiB（64MiB必須上限内、32MiB推奨目標超過） |
| gzip upload（参考） | 13,551.90KiB |
| global startup | 20ms（1,000ms必須上限内） |
| Wasm SHA-256 | `f7d62bb769e8b683ce4d9abc8eed36e53869f5a35eae7d9c60d9bf5ba8587e69` |
| JS SHA-256 | `ec3a17aac46878ead4316bcb1874908fe1cffda0de105c7e004f373217fc4a79` |

### HTTP・互換性

Workerへの検証要求は上限200件、同時数最大4、timeout30秒で実施した。
内訳は計測系列169件と、独立にbuildしたtagなしnative CLIとの比較18件・認証/不正入力13件。
期待したstatusは200が171件、413が14件、401が3件、400が10件、404/405が各1件で、不意のstatusや通信失敗は0件だった。
品質不合格を示す正常200と、runtime失敗は区別する。

日本語、簡体字・繁体字、mixed、Markdown/URL/code、絵文字、options、warningの昇格、空文字、1/10/100/256KiB、位置の異なる入力を比較した。
`pass/summary/issues`の全要素・順序に加え、通常metaも一致した。
除外したmetaは `validation_isolate_id`、`validation_request_sequence`、`validation_initialization`、`validation_wasm_memory_bytes` の4項目のみ。
parity用client時間にはNodeプロセス起動の時間も含むため、以下のlatency統計には混ぜていない。

### CPU・wall・client時間

client時間はrequest開始からheaders・本文取得・JSON解析・正常結果確認まで。
cold/warmは応答の初期化状態で分類し、クライアントの要求順からは決めない。
以下は取得できた小標本の値であり、P95はnearest rankの経験分位点。
欠落したCPUを0で補っていない。

| ケース | HTTP n / CPU n | client中央値 ms | client最大 ms | CPU中央値 ms | CPU最大 ms |
| --- | ---: | ---: | ---: | ---: | ---: |
| cold 1KiB | 3 / 1 | 1,796.82 | 1,949.54 | 1,123 | 1,123 |
| cold 10KiB | 1 / 1 | 3,320.75 | 3,320.75 | 2,199 | 2,199 |
| cold 256KiB（escape/並列を含む） | 23 / 22 | 2,999.03 | 5,095.85 | 1,456 | 2,414 |
| warm 1KiB | 58 / 6 | 152.28 | 169.15 | 3 | 7 |

計測系列の正常200は153件で、そのうちcold27件・warm126件。
cold27件のclient P95は4,119.28ms、最大5,095.85msで、今回の観測では10秒超過はなかった。
ただしcold 100KiBとwaitingは観測されていないため、全必須coldケースを確認済みとはしない。

cold 1KiBの取得CPUは1,123msで、保存済みlegacyの4,567msから約75.4%短縮した。
別時点・別isolateの観測比較であり、本番の制御されたA/B benchmarkではない。
1,000ms未満という最適化目標はこの1標本では超過し、cold 1KiB CPUの目標標本数30も未達。
warm 1KiB CPUの中央値3ms、P95/最大7msは取得6件の結果で、目標標本数100には届かない。

169計測要求のうち109件をmeasurement IDでlive tailと照合し、すべて対象version・outcome `ok` だった。
最初の4件はWebSocket接続完了を確認せずに送信したが、欠落原因が接続待ちだけだったとは断定しない。
WranglerのJSONモードにはprettyモードの接続完了メッセージがないため、接続確認を待つだけでHTTPを送らなかった試行も記録した。
その後は直接の `trace-v1` WebSocket openを確認してから送ったが、2件、さらに最後の54件でtailが欠落した。
収集器はtransport error/closeの詳細を永続化しておらず、原因は未特定。
Workerのobservability設定はnullで、保存済みログによる回収も設定されていなかった。
HTTP結果をCPU取得成功と混同せず、追加CPU測定の前に収集器の切断・配送状態の記録を改善する。

### 並列性

4要求を同時送信する8組で、すべて位置の異なる結果を正しく返した。
7組は4つの異なるisolateに分かれ、1組だけ4要求が3つのisolateへ届いた。
同じisolateの2要求はwarm、連番21/22で、tailの開始時刻とwall時間から得られる区間も重ならなかった。
クライアント側で重なっていても、実Worker内の同時処理や初期化待ちの証拠にはしない。

### メモリ観測と成立性

UTC 2026-09-10 05:55:15〜06:09:13を対象に、GraphQLでscript version・invocation status別に集計した。
最終時間窓には201 invocations、status `success` の行だけがあり、他の失敗statusは観測されなかった。
検証の200要求との一対一照合ではなく、時間窓集計として扱う。

| 観測項目 | bytes |
| --- | ---: |
| V8 isolate memory・観測max | 107,176,215 |
| V8 isolate memory・P50 | 91,840,950 |
| V8 isolate memory・P90 | 101,816,710 |
| V8 isolate memory・P99 | 104,690,696 |
| V8 isolate memory・P999 | 107,176,216 |
| Wasm memory・GraphQL max | 89,653,248 |
| Wasm capacity・応答でのmax | 89,653,248 |

V8観測maxは約107.18MB / 102.21MiBで、96MiBの参考線を超え、112MiB未満だった。
legacyの以前の時間窓max 99,435,263 bytesより高いが、今回は大量指摘など入力構成が異なるため、同条件でのpacked退行と断定しない。
P999とmaxの1byte差もAPIの返却値をそのまま保存した。

[Cloudflareの定義](https://developers.cloudflare.com/workers/observability/metrics-and-analytics/#memory-usage)では、memory usageはinvocation時点の共有isolateメモリの観測とsamplingであり、処理中の連続ピークではない。
Wasm容量とV8観測値は足し合わせない。
`peak_isolate_memory_bytes: null`、`peak_memory_evidence: unmeasured`、Issue #9の`insufficient_evidence`を維持する。

残作業は全体ピークの証拠、同一isolateで重なった初期化要求、cold 100KiBと十分なcold/warm CPU標本、tail欠落原因の切り分けである。
今回の検証ではWorkerの再デプロイ、secret変更、Issue #10の開始は行っていない。

## legacy版の過去判定

ユーザーがデプロイした改修版 `820f287a-573d-4b9c-925d-5021640dd2eb` の成功ログから、非圧縮 bundle `13,076.83 KiB` と global startup `35 ms` を確認した。
改修版と native CLI の結果互換性を追加で18/18件確認し、認証・不正入力13件、サイズ・境界・並列要求の3系列39件も期待した status だった。
既存の改修版 live tail 記録13件は測定ID・CPU・wall time・versionの対応が揃っており、warm 1 KiB の CPU は3 msだった。
認証更新後の最新系列でも13件のHTTP要求と live tail を照合し、CPU時間を取得できた。
途中の認証エラーとHTTPのみの測定は履歴として保持し、最新系列と分けて記録した。

JavaScript と Wasm を含むピーク時の isolate メモリは未取得である。
cold と warm は区別できたが、cold でクライアント経過時間が10秒を超える要求が再現した。
実 Worker の4並列要求は異なる isolate に届いたため、同一 isolate の同時処理の証拠にはしない。
必須条件の証拠が揃っていないため、Issue #9 は未解決とし、後続の Issue #10 も開始しない。

## packed CJモデルの実装・ローカル比較（デプロイ前の記録）

2026-09-10のレビュー反映計画に従い、完成済みCJ配列をビルド時に生成する方式を実装した。
通常native CLIはcanonical gzipを使い続け、Workerだけが `jpqg_packed_cjmodel` tagでsafe copy decoderへ切り替わる。
`Load()`のcacheと初期化失敗の契約は保持し、自動fallbackは追加しない。
形式・信頼境界・生成手順は [PACKED.md](../../internal/cj/PACKED.md) に記載した。

正本は `feasibility.json.packed_model_measurements`。
以下の3層を区別し、既存の実Worker記録は上書きしない。

| 判定の層 | 現在の結果 |
| --- | --- |
| Migration correctness | この端末で全配列ビット一致、全980,423 keyのlookup、分類・GateResult互換性、破損拒否、生成再現性、bootstrapを確認 |
| Packed optimization | ローカルcold短縮とWasm容量減少を確認。実WorkerのCPU/startupは未取得 |
| Issue #9 feasibility | 証拠不足。実Workerのclient 10秒、全体ピークメモリ、同一isolateで重なった要求の確認が残る |

### サイズと生成物

- canonical gzip SHA-256: `b0fcb1e82dac11d2e11710012b563f7b19ee3e92ce6a01e7de806bcaadfc012f`
- packed file SHA-256: `03f86a38687e8316f3bd97e109cc5864f68166ce5beb0df69be42fa20d3deccb`
- packed bytes: **29,205,984**。復元配列: **29,205,848 bytes**。
- embedded＋復元配列: **58,411,832 bytes**。モデル部分だけの予算であり、isolate全体の上界ではない。
- occupiedは980,423 / 2,097,152 slots。float64/float32、slot/offsetの全要素を維持する。
- 実サイズに基づきdecoderのfile/array防御上限を32MiBとした。要件の128MBをこの防御値で保証するものではない。

同じソースとGo 1.27.1、Wrangler 4.130.0、互換日付2026-09-09からtagだけを切り替えた。
Workerの `GOMEMLIMIT=48MiB` は双方で固定した。
出力を `.generated/{legacy,packed}` / `dist/{legacy,packed}` に分離し、JS/Wasm/glue/config/modelのhashをbuild manifestに残した。
基準commitは `8a7a1cf`、今回は未コミットの変更を含むため、その状態も明記した。

| dry-run upload | legacy | packed |
| --- | ---: | ---: |
| 非圧縮 KiB | 13,237.73 | 33,695.45 |
| gzip KiB（参考） | 9,086.97 | 13,576.78 |
| global startup | 未取得 | 未取得 |

packedは64MiBの必須上限内だが、32MiBの推奨目標を超える。
以前のデプロイ版の35msをpacked版のstartupに転用しない。

### ローカルHTTP比較

fresh workerdを6種類の初回ケース×5回×2方式、計60プロセス起動した。
各方式495要求、全990要求が期待statusで終了した。
HTTP超過による期待した413は測定失敗に数えず、不意のstatus・通信失敗は0件だった。
テスト等と実行が重なった先行60プロセスの予備系列は別保存し、この表に含めない。

各coldケースのnは5、値はclient経過時間の中央値。
小標本であり、母集団P95や絶対上限の保証には使わない。
正本にはmin/median/nearest-rank P95/max、初期化状態、失敗数、全要求の数値を保存した。

| 初回ケース | legacy ms | packed ms |
| --- | ---: | ---: |
| 1KiB | 1,172.88 | 376.12 |
| 10KiB | 1,167.98 | 379.61 |
| 100KiB | 1,205.38 | 396.70 |
| 256KiB | 1,196.21 | 421.25 |
| Unicode escape 256KiB | 1,222.95 | 430.86 |
| 4並列256KiB内のcold要求 | 1,199.21 | 429.62 |

cold 1KiBは約67.9%短縮したが、localの目安250msには未達。
warm 1KiBの中央値は3.78→3.78ms、256KiBは42.44→44.52msだった。
warm 1KiBのP95は5.81→10.15msと増えたため、中央値だけで全分布の退行なしとは扱わない。
本番のwarm CPU目標は未評価。

観測したWasm容量最大値は101,711,872→91,226,112 bytes（約97.00→87.00MiB）。
JS heapなどを含む全体ピークではなく、`peak_isolate_memory_bytes: null` を維持する。

初回4並列の各プロセスでは同じisolate IDを確認したが、初期化状態はcold1件＋warm3件だった。
waitingは観測されず、初期化中の重なりがこの実験で証明されたとは扱わない。
既存HTTPテストでは初期化cache、連番、結果混入なしを検証する。

### Native benchmarkとlocal profile

nativeはdarwin/arm64、Apple M4 Pro、Go 1.27.1、GOMEMLIMIT/GOGC指定なし。
入力file読込を除き、legacy parserと、checksum＋安全性検証を含むpacked decodeを各5回測定した。

| 中央値 | legacy | packed |
| --- | ---: | ---: |
| load ms/op | 244.60 | 19.09 |
| B/op | 190,634,420 | 29,212,833 |
| allocs/op | 1,983,156 | 7 |

時間は約92.2%、累積割当てbytesは約84.7%、割当て回数は99%以上減少した。
B/opは累積割当てでありpeak memoryではない。
packedのB/opと論理配列bytesの差は約6,985 bytesだった。

別のCDP profileでは、legacyのParseFloat・gzip・CJ parse・bigram insertionが目立った。
packedではSHA-256のself sampleが約127.21ms、DecodePackedが約28.72ms、検証走査が約20.03msだった。
これらは1回のlocalサンプル値で、正確な工程時間でもCloudflare課金CPUでもない。
UnihanのJSON/gzip処理とGo runtimeの処理は残る。
通常比較の計測中にはprofilerを接続せず、profile記録とbundle hashを別に保存した。

### 検証と残作業

`make check`（Go tests/vet、OMP 6/6、Pi 9/9、完全再生成、packed tag付きinternal tests、bootstrap）、Worker build/typecheck、HTTPを含むNodeテスト10/10が通った。
15秒のdecoder fuzzも通った。
全体テストは `JPQG_*` のprose設定を継承しない環境で実行した。
最初の実行では環境のtextlint設定により既存 `TestProseCLI` の「設定欠落」ケースが成立せず失敗したが、通常CLIのコード変更は行っていない。

独立レビューで指摘された入力言語列の厳密検証とカスタムパスのprovenanceを生成器側で修正した。
追加テストと完全再生成checkを通し、canonicalからのbin/manifestは変更されなかった。

リポジトリに既存CI定義はないため、外部CIの追加はせず `make check` に検証を組み込んだ。
異なるnative architectureでの生成一致は未確認。

このローカル記録の作成時点では、計画の「デプロイは運用者が明示的に実施する」に従い、packed版の追加デプロイを待っていた。
その後のデプロイと実測は冒頭の最新節に記録した。
validation設定は準備済みの `.generated/packed/index.ts` を指す。
運用者のデプロイ後にversion IDとbuild manifestを対応付け、tail CPU/wall、client時間、startup、メモリ時間窓集計を取得する。
初期の実Worker測定予算は1方式200要求・同時数4・timeout30秒を計画上の案とし、実行前に運用者と確定する。

10秒条件は添付計画に従い、request開始からheaders・本文取得・JSON解析・正常結果確認までのclient経過時間で評価する。
本番でcold 1KiB CPUが1秒以内になっても、その値をclient 10秒や全体メモリの代替にしない。
全体ピーク、同一isolateの重なりなど未取得証拠を残してIssue #9成立とせず、Issue #10も開始しない。

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
