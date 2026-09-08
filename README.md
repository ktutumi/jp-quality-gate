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

## Optional prose lint

`--textlint` と `--natural-japanese` を指定すると、Unihan → CJClassifier の後に、textlint → natural-japanese の順で有効なツールだけを実行します。
既定では両方とも無効で、Node.js・Python・uv などの外部 runtime は不要です。
有効時もツールの自動ダウンロードは行いません。各プロセスの制限時間は30秒です。

外部 lint の指摘は `textlint:<ruleId>` / `natural-japanese:<category>` として `issues[]` に追加し、すべて `warning` にします。
元ツールの severity にかかわらず、`--warnings-as-errors` または `JPQG_WARNINGS_AS_ERRORS=1` で `error` に昇格します。
実行ファイル・明示した設定ファイル・スクリプトの不在、不正な JSON、異常終了、タイムアウトは `internal_error` を含む JSON と exit `2` を返します。

### textlint の導入

リポジトリ内の `tools/textlint/` に、共用の実行ファイルと設定をまとめています。
**jp-quality-gate のリポジトリルート**で、依存パッケージをインストールしてください。

```bash
npm ci --prefix tools/textlint
```

推奨設定は [`tools/textlint/.textlintrc.json`](tools/textlint/.textlintrc.json) です。
`preset-ja-technical-writing` と `preset-ai-writing` を組み合わせ、`ai-tech-writing-guideline` は `severity: info` に設定しています。
`package-lock.json` でバージョンを固定し、`node_modules/` は Git の管理対象から除外しています。
Go バイナリの通常利用には、このインストールは不要です。

リポジトリルートで以下を設定すると、別のプロジェクトに移動しても共用できます。

```bash
export JPQG_TEXTLINT=1
export JPQG_TEXTLINT_BIN="$PWD/tools/textlint/node_modules/.bin/textlint"
export JPQG_TEXTLINT_CONFIG="$PWD/tools/textlint/.textlintrc.json"

cd /path/to/another-project
jp-quality-gate answer.md
```

毎回使う場合は、展開後の絶対パスをシェル設定に保存してください。
既存の `.textlintrc.*` を `--textlint-config` で指定することもできます。

`--textlint-bin` の既定は PATH 上の `textlint` です。`npx` は呼び出しません。
`--textlint-config` または `JPQG_TEXTLINT_CONFIG` は有効時に必須です。設定なしで textlint が無検査のまま成功することを防ぐため、省略時も exit `2` にします。
元ファイルの拡張子によらず `response.md` として stdin を解析します。

### natural-japanese の導入

[natural-japanese](https://github.com/coji/natural-japanese) を取得し、Python・uv と辞書を事前に準備します。
`lint.py` は同じディレクトリの `textcore.py` を使用するため、スクリプト単体ではなくリポジトリを取得してください。

```bash
git clone https://github.com/coji/natural-japanese.git /path/to/natural-japanese
export JPQG_NATURAL_JAPANESE_SCRIPT=/path/to/natural-japanese/skills/natural-japanese/scripts/lint.py

# 初回の準備時だけ、uv に依存パッケージと辞書を取得させる
uv run --no-project --script "$JPQG_NATURAL_JAPANESE_SCRIPT" answer.md --json

jp-quality-gate --natural-japanese --natural-japanese-genre tech answer.md
```

ゲートは `uv run --offline --no-python-downloads --no-project --script` で `lint.py` を呼び出します。
準備時と実行時で同じ uv キャッシュを使用してください。
入力は権限を制限した一時ファイルに書き込み、終了時に削除します。
指摘があっても `lint.py` は exit `0` を返すため、`findings` 配列から判定します。exit `1` は内部エラーです。
`--natural-japanese-genre` は `essay` / `tech` / `business`、未指定時は上流の共通プロファイルです。
`semantic.py`、`outline.py`、`terms.py` は実行しません。

### 環境変数と OMP / Pi

CLI 引数は対応する環境変数より優先します。有効化には `1` / `true` / `yes` / `on` を使えます。
ツールのパスを指定しただけでは有効になりません。

```text
--textlint                  JPQG_TEXTLINT
--textlint-bin              JPQG_TEXTLINT_BIN
--textlint-config           JPQG_TEXTLINT_CONFIG
--natural-japanese          JPQG_NATURAL_JAPANESE
--natural-japanese-script   JPQG_NATURAL_JAPANESE_SCRIPT
--natural-japanese-genre    JPQG_NATURAL_JAPANESE_GENRE
--uv-bin                    JPQG_UV_BIN
--warnings-as-errors        JPQG_WARNINGS_AS_ERRORS
```

ツールのパス設定後、同じ環境から OMP または Pi を起動します。

```bash
export JPQG_TEXTLINT=1
export JPQG_TEXTLINT_BIN=/absolute/path/to/node_modules/.bin/textlint
export JPQG_TEXTLINT_CONFIG=/absolute/path/to/.textlintrc.json
export JPQG_NATURAL_JAPANESE=1
export JPQG_WARNINGS_AS_ERRORS=1
omp  # または pi
```

両 adapter は `rule`・`source`・`message` を修正プロンプトへ渡します。
文章表現のみを修正し、技術的内容・コード・数値を維持する既存の方針を使います。
外部ツールの障害は既存どおり fail-open となり、通知後に自動修正を中止します。

### Markdown と位置情報

prose lint は `--include-code` にかかわらず fenced code・inline code・URL を除外します。
textlint は Markdown AST を利用し、URL を位置保持でマスクします。コード・URL の範囲だけを指す診断も除外します。
natural-japanese には既存のマスク処理でコードと URL を空白に置き換えた入力を渡します。

textlint の UTF-16 位置は、既存 schema の Unicode 文字単位の位置へ変換します。
natural-japanese は行番号と抜粋を返すため、抜粋がその行で一致する場合はその範囲を使用します。
統計などの指摘で一致しない場合は行頭の空範囲を使用し、`details.position_precision` を `line` にします。
元の抜粋は `details.excerpt` に残します。

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
bun test integrations/omp/index.test.js
node --test integrations/pi/index.test.js
```

まとめて実行:

```bash
make check
```

外部ツール不要の単体テストに加え、adapter テストでは Go CLI とテスト用 linter を使って修正ループを検証します（Go・Bun・Node.js が必要）。
インストール済み実ツールとの互換性テストは明示的に有効化できます。各ケースの実行時間も出力します。

```bash
JPQG_TEST_TEXTLINT_BIN=/absolute/path/to/node_modules/.bin/textlint \
JPQG_TEST_TEXTLINT_CONFIG=/absolute/path/to/.textlintrc.json \
JPQG_TEST_NATURAL_JAPANESE_SCRIPT=/absolute/path/to/scripts/lint.py \
go test ./internal/prose -run TestInstalledLinters -v
```

uv の場所を指定する場合は `JPQG_TEST_UV_BIN` を設定してください。
いずれかのツールだけでも検証でき、未指定のツールはスキップします。

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
  prose/
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
