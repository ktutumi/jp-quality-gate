# CJ packed model v1

> 作成日時: 2026-09-10 13:14

Worker専用の生成形式。
canonical sourceは `internal/embedded/data/cjlogprobs.gz`（CJClassifier 1.0.5）で、通常CLIは引き続きこのgzipを既存parserで読む。
`jpqg_packed_cjmodel` tag付きのビルドだけが非圧縮 `cjmodel-v1.bin` を埋め込み、safe copy decoderを使う。
Unihanのgzip処理は変更しない。

## バイト形式

整数・IEEE754ビット列はlittle endian。
ヘッダーは136 bytes固定で、paddingもtrailing bytesもない。

| Offset | Bytes | フィールド / v1値 |
| ---: | ---: | --- |
| 0 | 8 | Magic: ASCII `JPQGCJ01` |
| 8 | 4 | FormatVersion: 1 |
| 12 | 4 | Flags: 0 |
| 16 | 4 | CJRangeStart: 0x3400 |
| 20 | 4 | CJRangeEnd: 0x9FFF |
| 24 | 4 | LangCount: 3 |
| 28 | 4 | LanguageOrderID: 1 (`zh-hans, zh-hant, ja`) |
| 32 | 8 | DefaultLogProb: float64 bits |
| 40 | 8 | ToleratedKanaThreshold: float64 bits |
| 48 | 4 | UnigramCount: 82,944 |
| 52 | 4 | BigramMask: KeysCount − 1 |
| 56 | 4 | BigramKeysCount |
| 60 | 4 | BigramOffsetsCount: KeysCountと同じ |
| 64 | 4 | BigramProbCount |
| 68 | 4 | TableLayoutID: 1 |
| 72 | 32 | SourceModelSHA256: canonical gzip bytesのSHA-256 |
| 104 | 32 | ContentSHA256: 自フィールドをゼロとして全ファイルをhash |

payloadは `float64 unigrams → uint32 keys → uint32 offsets → float32 probs` の順。
保存するのはsliceのlen部分で、capacityやGoのpointerは保存しない。
ファイル長は `136 + 8*U + 4*K + 4*O + 4*P`。
チェックサムは `SHA256(data[:104] || 32 zero bytes || data[136:])` とする。

TableLayoutID=1は `key=(uint32(c1)<<16)|uint32(c2)`、次の32bit wrapping mixer、linear probing `(index+1)&Mask` を固定する。
言語順・key・mixer・probe規則を変更するときはformatの互換性を再判断する。

```go
key ^= key >> 16
key *= 0x85EBCA6B
key ^= key >> 13
```

## 検証境界

配列割当て前にheader、count、正確なfile length、checksum、32MiBのfile/array allocation budgetを検証する。
countはuint32からuint64へ拡張して演算するため、積和はuint64の範囲に収まる。
intへの変換前にも範囲を確認する。
実モデルが約27.85MiBだったため、計画の初期上限64MiBから32MiBへ絞った。
この防御値はisolate全体のメモリ上限ではない。

runtimeの線形検証は以下を要求する。

- KeysCountは16以上の2冪、OffsetsCountと同じ、MaskはKeysCount−1。
- occupiedは75%以下で、empty slotが存在する。
- empty keyのoffsetは0、occupied keyのoffsetは1以上、`(offset−1)%3 == 0`、`offset+3 <= ProbsCount`。
- `ProbsCount == 1 + 3*occupied`、`Probs[0]`のビット列は+0。
- 全floatは有限、kana thresholdは0〜1。

生成器のdeep validationは、重複key、重複・未参照probability block、probe到達性も検証する。
runtimeに大きな検証用mapを追加しない。
checksumは署名ではなく、任意の利用者が再hashしたモデルの意味を保証しない。
APIで任意のpackedファイルを受け付けない。

## 生成と再現性

```sh
make pack-cj
make check-cj-packed
```

生成器はworking directory内のパスだけを受け付け、manifestのsource/output名をそのdirectoryからの相対パスとして記録する。
通常はリポジトリrootから実行する。
言語ヘッダーは `zh-hans,zh-hant,ja` の3列・順序一致を生成前に要求し、欠落・重複・順序変更を拒否する。
生成器はsourceを一度読み、そのbytesからhashとgzip readerを作り、既存 `ParseModel` を呼ぶ。
生成物が存在しなくてもtagなしのnative packerは起動できる。
同じparser・toolchain・sourceでbinとmanifestを再生成し、`--check`では既存の両ファイルと完全一致を要求する。
Go versionもmanifestの比較対象なので、toolchain変更時は差分をレビューする。
manifestに時刻・host・絶対pathは含めない。

出力先と同じdirectoryのtempへ書き、close、decode、全要素ビット一致を確認してからrenameする。
binとmanifestの2回のrenameは一括transactionではない。
途中失敗による不整合はWorker buildと完全再生成checkで拒否する。
生成中にWorker buildを並行実行しない。

`make check-cj-packed`は別の一時コピーで、生成物欠落からのbootstrap、各tagのEmbedFiles、parser変更後のstale artifactも確認する。
既存のCI定義はないため、新しい外部CIは作らず `make check` に検証を追加した。
異なるarchitectureでのbyte一致はこの端末では未検証。

Worker buildはmanifest・source/file/content hashの軽量確認を行う。
parser変更によるstale artifactの最終検出は完全再生成checkの責務で、軽量確認だけでは代替できない。

## 実モデルの配列予算

| 項目 | 値 |
| --- | ---: |
| canonical gzip bytes | 7,597,074 |
| unigram要素 | 82,944 |
| key / offset要素（各） | 2,097,152 |
| probability要素 | 2,941,270 |
| occupied bigram | 980,423 |
| decoded配列 bytes | 29,205,848 |
| packed file bytes | 29,205,984 |
| embedded + decoded配列 bytes | 58,411,832 |

入力packed bytesと復元配列は同時に存在する。
この表にはGo runtime、Unihan、JS heap、request/responseなどを含まない。
モデル部分の予算をisolate全体の保守的上界として使わない。
