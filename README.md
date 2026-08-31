# jp-quality-gate v1

LLM が生成した日本語に対して、次の2段だけを実行する軽量品質ゲートです。

1. **Unihan 静的文字テーブル**: 中国簡体字の可能性が高い字形を文字単位で検出
2. **CJClassifier**: 文・節単位で Japanese / Chinese Simplified / Chinese Traditional を分類

この v1 は文体・流暢性の評価をしません。textlint / LangCheck 等はまだ含めていません。

## 判定ポリシー

Unihan 層は Unicode のプロパティを「日本語として不正である」という規範判定には使いません。
LLM 出力の品質ゲート用ヒューリスティックとして次のように使います。

- **error: `simplified_chinese_form`**
  - `kTraditionalVariant` がある
  - かつ強い日本側根拠がない
- **warning: `chinese_han_without_japanese_source`**
  - `kIRG_GSource` がある
  - かつ強い日本側根拠がない

強い日本側根拠として、`kIRG_JSource`, JIS mappings, 常用/人名用漢字,
`kIBMJapan`, `kMojiJoho`, Adobe-Japan1-6, 日本語読み、Unicode 18.0 の
`kJapaneseNewVariant` / `kJapaneseOldVariant` を whitelist に使います。

`kTraditionalVariant` は中国語の簡体字/繁体字関係を表すため、日本語新字体との混同を避けるために
日本側根拠を必ず確認します。

## Install

```bash
uv venv
source .venv/bin/activate
uv pip install -e .
```

`cjclassifier==1.0.5` は pure Python、ランタイム外部依存なしで、学習済みモデルをパッケージに含みます。

## 1. Unihan テーブルを一度生成

Unicode 18.0.0 を既定にしています。

```bash
jpqg-build-unihan
```

既定出力:

```text
~/.cache/jp-quality-gate/unihan-suspicious-18.0.0.json
```

ネットワークアクセスさせたくない場合は Unicode 公式の `Unihan.zip` を先に取得し、

```bash
jpqg-build-unihan \
  --version 18.0.0 \
  --unihan-zip /path/to/Unihan.zip
```

としてください。

## 2. Hook からチェック

stdin が最も簡単です。

```bash
printf '%s' 'これは经済に関する説明です。' | jp-quality-gate --pretty
```

終了コード:

- `0`: error なし
- `1`: 品質ゲート error あり
- `2`: 設定/依存/IO など内部エラー

JSON 例:

```json
{
  "pass": false,
  "summary": {"errors": 1, "warnings": 0, "issues": 1},
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

## CJClassifier の既定値

```text
--cj-min-cjk 4
--cj-min-gap 0.15
```

`gap` に公式推奨閾値は確認できないため、`0.15` は **初期運用値** です。
実際のローカルLLM出力を corpus にして誤検知/見逃しを測定し、調整してください。

- Chinese 判定かつ `gap >= cj-min-gap`: error
- Chinese 判定だが `gap < cj-min-gap`: warning

回答全体だけでは日本語部分に埋もれるため、文と節の両方を分類します。

## Markdown

既定では次を検査対象から外します。

- fenced code block
- inline code
- URL

コーディングエージェントの回答でソースコード中の文字列を誤検知しないためです。
コードも検査したい場合:

```bash
jp-quality-gate --include-code
```

## Hook 再生成ループ

推奨フロー:

```text
assistant final text
  -> jp-quality-gate
     -> exit 0: accept
     -> exit 1: JSON diagnostics を LLM に返して日本語だけ修正
                -> 再チェック（最大1〜2回）
     -> exit 2: hook 自体の障害として扱う
```

`examples/hook.sh` はハーネス非依存の最小例です。

## Harness integrations

### Oh My Pi (OMP)

OMP の `session_stop` Extension を使い、品質エラーを検出した場合に
同じターンを自動 continuation して回答を修正できます。

- Setup: [`integrations/omp/README.md`](integrations/omp/README.md)
- Extension: [`integrations/omp/index.js`](integrations/omp/index.js)
- 既定の自動修正回数: 2回
- CLI/integration error: fail-open

## Tests

```bash
uv pip install -e '.[dev]'
pytest -q
ruff check .
```

テストのうち Unihan 層は外部ネットワーク不要です。CJClassifier 本体の実測テストは依存導入後に追加してください。
