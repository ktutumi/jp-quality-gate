# jp-quality-gate

LLM が生成した日本語に対して、軽量な品質ゲートを提供します。

1. **Unihan 静的文字テーブル** — 中国簡体字の可能性が高い字形を文字単位で検出
2. **CJClassifier** — 文・節単位で Japanese / Chinese Simplified / Chinese Traditional を分類

実装は **pure Go** です。`jp-quality-gate` バイナリには CJClassifier モデルと Unicode 18.0.0 用の既定 Unihan テーブルを埋め込んでいるため、通常実行時に Python、uv、pip、cgo、JVM、Rust runtime、ネットワークアクセスは不要です。

## 判定ポリシー

Unihan 層は Unicode のプロパティを「日本語として不正である」という規範判定には使いません。LLM 出力の品質ゲート用ヒューリスティックとして利用します。

- **error: `simplified_chinese_form`**
  - `kTraditionalVariant` がある
  - かつ強い日本側根拠がない
- **warning: `chinese_han_without_japanese_source`**
  - `kIRG_GSource` がある
  - かつ強い日本側根拠がない

強い日本側根拠として、`kIRG_JSource`, JIS mappings, 常用/人名用漢字, `kIBMJapan`, `kMojiJoho`, Adobe-Japan1-6, 日本語読み、`kJapaneseNewVariant` / `kJapaneseOldVariant` などを whitelist に使います。

## Requirements

ビルドには **Go 1.24+** が必要です。

実行時は `jp-quality-gate` 単体で動作します。

## Build / install

```bash
git clone https://github.com/ktutumi/jp-quality-gate.git
cd jp-quality-gate

make
```

生成物:

```text
bin/jp-quality-gate
bin/jpqg-build-unihan
```

`~/.local/bin` へインストール:

```bash
make install
```

別の場所へ入れる場合:

```bash
make install GOBIN=/path/to/bin
```

確認:

```bash
which jp-quality-gate
jp-quality-gate --help
```

## Usage

stdin:

```bash
printf '%s' 'これは经済に関する説明です。' | jp-quality-gate --pretty
```

ファイル:

```bash
jp-quality-gate answer.md
```

明示的に stdin:

```bash
cat answer.md | jp-quality-gate -
```

品質エラーがある場合は JSON を stdout に出力し、exit code `1` になります。

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
      "text": "经",
      "line": 1,
      "column": 3,
      "details": {
        "codepoint": "U+7ECF",
        "traditional_variants": ["經"],
        "japanese_candidates": ["経"]
      }
    }
  ]
}
```

### Exit codes

```text
0 = quality error なし
1 = quality gate error あり
2 = CLI / config / model / I/O などの内部エラー
```

内部エラー時も stdout に JSON を出力します。

```json
{"pass":false,"internal_error":"..."}
```

## CLI options

```text
jp-quality-gate [file]

--unicode-version VERSION
--unihan-table PATH
--cj-min-cjk N
--cj-min-gap FLOAT
--include-code
--warnings-as-errors
--pretty
```

既定値:

```text
--unicode-version 18.0.0
--cj-min-cjk 4
--cj-min-gap 0.15
```

`--cj-min-cjk` は1以上、`--cj-min-gap` は0.0以上1.0以下です。

## Embedded data

通常利用では `jpqg-build-unihan` の実行は不要です。

Unihan テーブルの解決順序:

1. `--unihan-table`
2. `JPQG_UNIHAN_TABLE`
3. Unicode `18.0.0` の埋め込みテーブル
4. その他 version のキャッシュ (`XDG_CACHE_HOME`、未設定時は `~/.cache/jp-quality-gate`)

埋め込みデータ:

- CJClassifier model: `internal/embedded/data/cjlogprobs.gz`
- Unihan table: `internal/embedded/data/unihan-suspicious-18.0.0.json.gz`

Unicode 18.0.0 の埋め込みテーブルは Go port 実装時に利用した snapshot から生成されています。正式データへ更新する場合は `jpqg-build-unihan` で再生成してください。

## Custom Unihan table

別 Unicode version や独自テーブルを使う場合:

```bash
jpqg-build-unihan \
  --version 18.0.0 \
  --unihan-zip /path/to/Unihan.zip \
  --output /path/to/unihan.json
```

その後:

```bash
jp-quality-gate --unihan-table /path/to/unihan.json
```

## CJClassifier

CJClassifier の既定値:

```text
--cj-min-cjk 4
--cj-min-gap 0.15
```

- Chinese 判定かつ `gap >= cj-min-gap`: error
- Chinese 判定だが `gap < cj-min-gap`: warning

回答全体だけでは日本語部分に埋もれるため、文と節の両方を分類します。

Go 版には `cjclassifier==1.0.5` 相当の分類ロジックとモデルを移植しています。third-party attribution は [`third_party/cjclassifier/`](third_party/cjclassifier/) を参照してください。

## Markdown

既定では次を検査対象から外します。

- fenced code block
- inline code
- URL

コードも検査する場合:

```bash
jp-quality-gate --include-code
```

## Harness integrations

### Oh My Pi (OMP)

OMP の `session_stop` Extension で最終回答を検査し、品質エラー時は同じターンを自動 continuation して修正します。

- Setup: [`integrations/omp/README.md`](integrations/omp/README.md)
- Extension: [`integrations/omp/index.js`](integrations/omp/index.js)
- 既定の自動修正回数: 2回
- CLI / integration error: fail-open

### Pi coding agent

Pi の `turn_end` と steering queue を使い、ツール呼び出しのない最終 assistant 応答を検査します。

- Setup: [`integrations/pi/README.md`](integrations/pi/README.md)
- Extension: [`integrations/pi/index.js`](integrations/pi/index.js)
- 既定の自動修正回数: 2回
- 品質修正は queued follow-up より先に処理
- 過去の correction message は後続 LLM context から除外
- CLI / integration error: fail-open

両 integration から別のバイナリを使う場合は `JPQG_BIN` を指定できます。

```bash
export JPQG_BIN=/absolute/path/to/jp-quality-gate
```

## Tests

Go core:

```bash
go test ./...
go vet ./...
```

OMP / Pi integration:

```bash
node --test integrations/omp/index.test.js
node --test integrations/pi/index.test.js
```

まとめて実行:

```bash
make check
```

## Project structure

```text
cmd/
  jp-quality-gate/
  jpqg-build-unihan/
internal/
  cj/
  embedded/
  gate/
  report/
  text/
  unihan/
integrations/
  omp/
  pi/
third_party/
  cjclassifier/
  unicode/
```

## License

本プロジェクト本体は MIT License です。

同梱する CJClassifier および Unicode 由来データについては、それぞれ `third_party/` 配下のライセンス・NOTICE を参照してください。
