# Pi integration

`jp-quality-gate` を Pi coding agent の最終応答に適用する Extension です。

Pi には OMP の `session_stop` に相当する continuation hook がないため、`turn_end` で
**ツール呼び出しを含まない assistant turn** を検査し、品質エラー時は hidden custom message を
`followUp` としてキューへ入れます。Pi の agent loop は `turn_end` 後に follow-up queue を確認するため、
同じ agent run の中で修正ターンが自動実行されます。

## 動作

```text
assistant final turn
  -> turn_end
  -> jp-quality-gate
     -> exit 0: accept
     -> exit 1: hidden correction message を followUp
                -> LLM が回答全体を日本語だけ修正
                -> 再チェック（既定で最大2回）
     -> exit 2 / malformed JSON: integration error を通知して fail-open
```

以下の turn は検査しません。

- tool call を含む assistant turn
- `toolResults` がある turn
- `stopReason` が `error` / `aborted`
- テキストがない assistant message

ツール実行後の最終テキスト応答は、次の tool call なし `turn_end` で検査されます。

## 前提

`jp-quality-gate` CLI が Pi から実行できる必要があります。

開発中はリポジトリルートで:

```bash
uv tool install --editable .
uv tool update-shell

which jp-quality-gate
jp-quality-gate --help
```

既存の `.venv` を使う場合は:

```bash
export JPQG_BIN=/absolute/path/to/jp-quality-gate/.venv/bin/jp-quality-gate
```

Unihan table が未生成なら:

```bash
jpqg-build-unihan
```

## 一時的に読み込む

最初は `-e` で動作確認することを推奨します。

```bash
pi -e /absolute/path/to/jp-quality-gate/integrations/pi/index.js
```

## 恒久的に導入する

### 方法1: local Pi package として install

`integrations/pi/package.json` に `pi.extensions` を定義しているので、ローカル package として導入できます。

```bash
pi install /absolute/path/to/jp-quality-gate/integrations/pi
```

プロジェクトローカル設定に入れる場合:

```bash
pi install -l /absolute/path/to/jp-quality-gate/integrations/pi
```

### 方法2: settings.json の `extensions`

`~/.pi/agent/settings.json`:

```json
{
  "extensions": [
    "/absolute/path/to/jp-quality-gate/integrations/pi/index.js"
  ]
}
```

プロジェクト単位なら `.pi/settings.json` に同じ設定を入れます。

## 動作確認

CLI 単体:

```bash
printf '%s' 'これは经済に関する説明です。' | jp-quality-gate --pretty
echo $?
```

Pi を Extension 付きで起動:

```bash
pi -e /absolute/path/to/jp-quality-gate/integrations/pi/index.js
```

テスト用プロンプト例:

```text
次の文をそのまま1行だけ回答してください。
これは经済に関する説明です。
```

最初の回答に `经` が残った場合、Pi の通知に次のような表示が出て、自動修正ターンが続きます。

```text
jp-quality-gate: 日本語品質エラーを検出。自動修正 1/2
```

修正指示は `display: false` の custom message なので、通常のユーザー発言としてUIには表示されません。

## correction message のコンテキスト管理

Pi の custom message はセッションに保存され、通常は以後の LLM context にも参加します。
この Extension は `context` event で `jp-quality-gate-correction` message をフィルタします。

- 修正中: 最新の correction message 1件だけを保持
- 修正完了後: 過去の correction message を LLM context から除外

セッションログには診断を残しつつ、後続会話への不要な影響を抑える設計です。

## 設定

OMP integration と同じ環境変数を使います。

| 変数 | 既定 | 説明 |
| --- | --- | --- |
| `JPQG_BIN` | `jp-quality-gate` | CLI の実行パス |
| `JPQG_MAX_RETRIES` | `2` | 自動修正回数。0〜7に clamp |
| `JPQG_CJ_MIN_CJK` | CLI既定 | CJClassifier の最小 CJK 文字数 |
| `JPQG_CJ_MIN_GAP` | CLI既定 | CJClassifier gap 閾値 |
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
- CLI が exit 2
- stdout が JSON ではない
- JSON schema が期待形式と一致しない
- `pass` / `summary` / `issues` の件数が矛盾する

品質チェッカー自身の障害で Pi の agent loop を止めないためです。

## テスト

```bash
cd integrations/pi
npm test
node --check index.js
```

Node.js 組み込みの test runner のみを使い、npm dependency はありません。

## OMP integration との違い

| | OMP | Pi |
| --- | --- | --- |
| Hook point | `session_stop` | `turn_end` |
| 自動継続 | `{ continue, additionalContext }` | hidden custom `followUp` |
| 診断の永続化 | continuation context | custom message として session に保存 |
| 後続 context | OMP 側の continuation | `context` event で過去診断を除外 |
| AbortSignal | `session_stop.signal` を利用 | `turn_end` には公開 signal がないため未使用 |

Pi 版は main coding-agent session の応答を対象にします。subagent Extension や外部プロセスの出力を直接検査するものではありません。
