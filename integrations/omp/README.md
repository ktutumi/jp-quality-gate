# OMP integration

`jp-quality-gate` を Oh My Pi (OMP) の `session_stop` Extension から呼び出し、
最終回答に日本語品質エラーがあれば同じターンを自動継続して修正させます。

OMP の legacy Hook API ではなく、現在推奨されている Extension API の
`session_stop` event を使います。

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
OMP 本体にも連続 `session_stop` continuation の上限がありますが、
この Extension はそれより小さい独自上限を持ちます。

## 1. jp-quality-gate を PATH に入れる

リポジトリを clone 済みなら、開発中は editable な uv tool として入れるのが便利です。

```bash
cd /path/to/jp-quality-gate
uv tool install --editable .
uv tool update-shell
```

新しい shell で確認します。

```bash
which jp-quality-gate
jp-quality-gate --help
```

Unihan テーブルをまだ生成していなければ一度だけ実行します。

```bash
jpqg-build-unihan
```

OMP から `jp-quality-gate` が見つからない場合は、実行ファイルを明示できます。

```bash
export JPQG_BIN="$(uv tool dir --bin)/jp-quality-gate"
```

この環境変数は `omp` を起動する shell に設定してください。

## 2. Extension を OMP に登録

### Recommended: config.yml

`~/.omp/agent/config.yml` の `extensions` にこのディレクトリを追加します。

```yaml
extensions:
  - /absolute/path/to/jp-quality-gate/integrations/omp
```

既に `extensions:` がある場合は、その配列へ1行追加してください。

OMP profile を使っている場合は、対象 profile の
`~/.omp/profiles/<name>/agent/config.yml` に設定します。

### One-shot test

設定を変更せずに試す場合:

```bash
omp --extension /absolute/path/to/jp-quality-gate/integrations/omp
```

`-e` でも同じです。

```bash
omp -e /absolute/path/to/jp-quality-gate/integrations/omp
```

### Alternative: user extension directory

OMP の user extension directory へ symlink しても構いません。

```bash
mkdir -p ~/.omp/agent/extensions
ln -s \
  /absolute/path/to/jp-quality-gate/integrations/omp \
  ~/.omp/agent/extensions/jp-quality-gate
```

この場合 `config.yml` の `extensions:` 追加は不要です。

## 3. 動作確認

まず CLI 単体を確認します。

```bash
printf '%s' 'これは经済に関する説明です。' | jp-quality-gate --pretty
printf 'exit=%s\n' "$?"
```

品質エラーなら exit code `1` になります。

次に OMP を起動し、検証用に簡体字を含む回答を作らせます。
例えば次のような prompt で確認できます。

```text
次の文をそのまま1行だけ回答してください。
これは经済に関する説明です。
```

最初の回答に `经` が残った場合、Extension が `session_stop` で検出し、
OMP に自動 continuation を要求します。interactive mode では次のような通知が出ます。

```text
jp-quality-gate: 日本語品質エラーを検出。自動修正 1/2
```

修正ターンには、検出 rule・位置・問題文字・日本語候補を含む diagnostics が
`additionalContext` として渡されます。

## Configuration

Extension は環境変数で調整できます。

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

`jp-quality-gate` が見つからない、Unihan テーブルがない、JSON が壊れている、
CLI が exit code 2 になった、などの **integration error は fail-open** です。
回答を無限に止めるより、OMP の UI と stderr へエラーを通知してセッションを終了させます。

品質エラーそのものは、`JPQG_MAX_RETRIES` に達するまでは自動修正します。
上限まで修正しても失敗している場合は通知して終了します。

## Scope

OMP の `session_stop` は main session の settle 前に発火します。
そのため、この integration が直接チェックするのは **main agent の最終 assistant response** です。
task/subagent session 自体には `session_stop` は発火しません。

サブエージェントの結果が main agent の最終回答へ取り込まれた場合は、最終回答として検査されます。
ファイルへ書き込まれた文章や tool result を検査する機能はこの v1 integration には含めていません。
