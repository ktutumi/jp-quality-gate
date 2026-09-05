# OMP integration

`jp-quality-gate` を Oh My Pi (OMP) の `session_stop` Extension から呼び出し、最終回答に日本語品質エラーがあれば同じターンを自動継続して修正させます。

OMP の legacy Hook API ではなく Extension API の `session_stop` event を使います。

## Behavior

```text
assistant final response
  -> OMP session_stop
     -> jp-quality-gate
        -> exit 0: そのまま終了
        -> exit 1: diagnostics を additionalContext としてモデルへ返す
                   -> 回答を再生成
                   -> 再チェック
        -> exit 2: Extension エラーを通知して fail-open
```

自動修正は既定で最大 **2回** です。

## 1. jp-quality-gate をインストール

リポジトリを clone 済みなら:

```bash
cd /path/to/jp-quality-gate
make install
```

既定のインストール先は `~/.local/bin` です。

確認:

```bash
which jp-quality-gate
jp-quality-gate --help
```

Go バイナリには CJClassifier model と既定 Unihan table が埋め込まれているため、通常利用では `jpqg-build-unihan` の事前実行は不要です。

PATH を変更したくない場合は、リポジトリ内で build して `JPQG_BIN` を指定できます。

```bash
make
export JPQG_BIN=/absolute/path/to/jp-quality-gate/bin/jp-quality-gate
```

この環境変数は `omp` を起動する shell に設定してください。

## 2. Extension を OMP に登録

### Recommended: config.yml

`~/.omp/agent/config.yml`:

```yaml
extensions:
  - /absolute/path/to/jp-quality-gate/integrations/omp
```

OMP profile を使う場合は `~/.omp/profiles/<name>/agent/config.yml` に設定します。

### One-shot test

```bash
omp --extension /absolute/path/to/jp-quality-gate/integrations/omp
```

または:

```bash
omp -e /absolute/path/to/jp-quality-gate/integrations/omp
```

### Alternative: user extension directory

```bash
mkdir -p ~/.omp/agent/extensions
ln -s \
  /absolute/path/to/jp-quality-gate/integrations/omp \
  ~/.omp/agent/extensions/jp-quality-gate
```

## 3. 動作確認

CLI 単体:

```bash
printf '%s' 'これは经済に関する説明です。' | jp-quality-gate --pretty
printf 'exit=%s\n' "$?"
```

品質エラーなら exit code `1` になります。

OMP でのテスト用 prompt:

```text
次の文をそのまま1行だけ回答してください。
これは经済に関する説明です。
```

最初の回答に `经` が残った場合、Extension が `session_stop` で検出して自動 continuation を要求します。interactive mode では次のような通知が出ます。

```text
jp-quality-gate: 日本語品質エラーを検出。自動修正 1/2
```

## Configuration

| Variable | Default | Description |
| --- | ---: | --- |
| `JPQG_BIN` | `jp-quality-gate` | CLI executable path |
| `JPQG_MAX_RETRIES` | `2` | 自動修正回数。0〜7に clamp |
| `JPQG_CJ_MIN_CJK` | CLI default | `--cj-min-cjk` を上書き |
| `JPQG_CJ_MIN_GAP` | CLI default | `--cj-min-gap` を上書き |
| `JPQG_WARNINGS_AS_ERRORS` | off | `--warnings-as-errors` を有効化 |
| `JPQG_DIAGNOSTIC_LIMIT` | `20` | モデルへ返す error 件数の上限 |

例:

```bash
export JPQG_MAX_RETRIES=1
export JPQG_CJ_MIN_GAP=0.20
omp
```

## Correction policy

再生成時にはモデルへ次の方針を渡します。

- 回答全体を自然な日本語で再出力する
- 検出された中国語字形・中国語表現を日本語へ修正する
- 技術的な内容と結論は変えない
- コード、コマンド、パス、識別子、URL、数値は変えない
- コードブロックは必要がなければ変更しない

## Failure policy

CLI が見つからない、JSON が壊れている、exit code `2` などの integration error は **fail-open** です。品質エラーそのものは `JPQG_MAX_RETRIES` に達するまで自動修正します。

## Scope

この integration が直接チェックするのは **main agent の最終 assistant response** です。サブエージェント結果が main agent の最終回答へ取り込まれれば最終回答として検査されますが、ファイルへ書き込まれた文章や tool result 自体は対象外です。

## Test

```bash
bun test integrations/omp/index.test.js
```
