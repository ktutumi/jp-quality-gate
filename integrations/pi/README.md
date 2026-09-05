# Pi integration

`jp-quality-gate` を Pi coding agent の最終応答に適用する Extension です。

Pi には OMP の `session_stop` に相当する continuation hook がないため、`turn_end` で **ツール呼び出しを含まない assistant turn** を検査し、品質エラー時は hidden custom message を `steer` としてキューへ入れます。Pi は turn 完了後に steering queue を follow-up queue より先に処理するため、別の follow-up が既に待機していても日本語修正を先に実行できます。

## Behavior

```text
assistant final turn
  -> turn_end
  -> jp-quality-gate
     -> exit 0: accept
     -> exit 1: hidden correction message を steer
                -> LLM が回答全体を日本語だけ修正
                -> 再チェック（既定で最大2回）
     -> exit 2 / malformed JSON: integration error を通知して fail-open
```

次の turn は検査しません。

- tool call を含む assistant turn
- `toolResults` がある turn
- `stopReason` が `error` / `aborted`
- テキストがない assistant message

## 1. jp-quality-gate をインストール

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

Go バイナリには CJClassifier model と既定 Unihan table が埋め込まれているため、通常利用では事前データ生成は不要です。

PATH を変更したくない場合:

```bash
make
export JPQG_BIN=/absolute/path/to/jp-quality-gate/bin/jp-quality-gate
```

## 2. 一時的に読み込む

```bash
pi -e /absolute/path/to/jp-quality-gate/integrations/pi/index.js
```

## 3. 恒久的に導入する

### local Pi package

```bash
pi install /absolute/path/to/jp-quality-gate/integrations/pi
```

プロジェクトローカル:

```bash
pi install -l /absolute/path/to/jp-quality-gate/integrations/pi
```

### settings.json

`~/.pi/agent/settings.json`:

```json
{
  "extensions": [
    "/absolute/path/to/jp-quality-gate/integrations/pi/index.js"
  ]
}
```

プロジェクト単位なら `.pi/settings.json` に同じ設定を入れます。

## 4. 動作確認

CLI 単体:

```bash
printf '%s' 'これは经済に関する説明です。' | jp-quality-gate --pretty
echo $?
```

Pi を Extension 付きで起動:

```bash
pi -e /absolute/path/to/jp-quality-gate/integrations/pi/index.js
```

テスト用 prompt:

```text
次の文をそのまま1行だけ回答してください。
これは经済に関する説明です。
```

最初の回答に `经` が残った場合、次のような通知が出て自動修正ターンが続きます。

```text
jp-quality-gate: 日本語品質エラーを検出。自動修正 1/2
```

修正指示は `display: false` の custom message なので、通常のユーザー発言としてUIには表示されません。

## correction message のコンテキスト管理

Pi の custom message はセッションに保存されます。この Extension は `context` event で `jp-quality-gate-correction` message をフィルタします。

- 修正中: 最新の correction message 1件だけを保持
- 修正完了後: 過去の correction message を LLM context から除外

## Configuration

| Variable | Default | Description |
| --- | ---: | --- |
| `JPQG_BIN` | `jp-quality-gate` | CLI executable path |
| `JPQG_MAX_RETRIES` | `2` | 自動修正回数。0〜7に clamp |
| `JPQG_CJ_MIN_CJK` | CLI default | CJClassifier の最小 CJK 文字数 |
| `JPQG_CJ_MIN_GAP` | CLI default | CJClassifier gap 閾値 |
| `JPQG_WARNINGS_AS_ERRORS` | false | warning も error 扱い |
| `JPQG_DIAGNOSTIC_LIMIT` | `20` | LLM に返す error 最大件数 |

例:

```bash
export JPQG_MAX_RETRIES=1
export JPQG_CJ_MIN_GAP=0.20
pi
```

## fail-open

次の場合は品質違反として再生成せず、integration error を通知して回答をそのまま受け入れます。

- `jp-quality-gate` が見つからない
- CLI が exit `2`
- stdout が JSON ではない
- JSON schema が期待形式と一致しない
- `pass` / `summary` / `issues` が矛盾する

## OMP integration との違い

| | OMP | Pi |
| --- | --- | --- |
| Hook point | `session_stop` | `turn_end` |
| 自動継続 | `{ continue, additionalContext }` | hidden custom `steer` |
| 診断の永続化 | continuation context | custom message として session に保存 |
| 後続 context | OMP 側の continuation | `context` event で過去診断を除外 |
| AbortSignal | `session_stop.signal` を利用 | `turn_end` には公開 signal がないため未使用 |

Pi 版は main coding-agent session の応答を対象にします。subagent Extension や外部プロセスの出力を直接検査するものではありません。

## Test

```bash
node --test integrations/pi/index.test.js
```
