# miko-post MVP Design Document（日本語訳）

> この文書は `design.md`（英語版）の日本語訳です。差異がある場合は英語版を正とします。

- ステータス: MVP仕様確定版
- 対象バージョン: v0.1
- アプリ名: `miko-post`
- CLIコマンド: `mp`
- 実装技術: Go + Fyne

## 1. 目的

`miko-post` は、思いついた一言を極力少ない操作で記録・投稿する小さなツールである。1つのメッセージを、TelegramとObsidian Daily Noteの2つの独立した投稿先（sink）へ送る。

CLIと最小構成のGUIは同じ投稿処理を共有する。MVPでは、入力の速さ、ローカルへの記録、単純な運用、障害発生後に人間やAIが調査できる構造化ログを重視する。

## 2. スコープ

### 2.1 MVPに含めるもの

- 1つのバイナリでCLIとGUIを提供する
- 引数なしの `mp` でGUIを起動する
- メッセージ引数付きの `mp` でCLI投稿する
- TelegramとObsidianを独立したsinkとして並行処理する
- 一方のsinkが失敗しても他方の処理を中止しない
- 全sinkの結果を収集し、表示・記録する
- 設定ファイルと状態ファイルのパスをXDG仕様に準拠させる
- CLI投稿時のみ任意の設定ファイルを指定できる
- JSONL形式の構造化ログと、サイズまたは経過日数によるローテーションを提供する

### 2.2 v0.1の対象外

- 画像投稿
- 設定GUI
- Telegram失敗投稿のretry queue／自動再送
- 標準入力（stdin）からの投稿
- HTTPの自動リトライ
- `--no-telegram`、`--no-obsidian` などの実行時sink切り替え
- ローテーション済みログの自動削除／retention
- 新しいsinkの追加

## 3. 全体構成

```text
CLI ----\
         >-- Post(message) --+--> sink.telegram
GUI ----/                    +--> sink.obsidian
             |                    （並行・独立）
             +--> JSONL logging
```

CLIとGUIは薄い入口とし、設定読み込み、入力検証、共通投稿サービスの呼び出し、集約結果の表示だけを担う。投稿コア、各sink、設定、GUI、loggingは責務を分ける。具体的なパッケージ構造は実装詳細とする。

## 4. CLI

### 4.1 構文

```text
mp [options] [message...]
```

```text
-c, --config PATH   CLI投稿で使用する設定ファイル
-h, --help          ヘルプを表示
```

### 4.2 起動規則

| 呼び出し | 動作 |
|---|---|
| `mp` | XDG既定設定でGUIを起動 |
| `mp "hello"` | XDG既定設定でCLI投稿 |
| `mp hello world` | 引数を半角スペースで結合し、`hello world` をCLI投稿 |
| `mp -c ./config.toml "hello"` | 指定設定でCLI投稿 |
| `mp -c ./config.toml` | エラーを表示し、GUIを開かず終了コード `1` |
| `mp --help` | 解決済みの既定設定パスを含むヘルプを表示 |

`--config` はCLI投稿専用であり、GUIが使う設定ファイルを変更しない。メッセージなしで指定された場合は次のエラーとする。

```text
--config is only available when posting from CLI
```

ヘルプには `$XDG_CONFIG_HOME` の式ではなく、現在の環境で実際に解決した既定パスを表示する。

```text
Usage:
  mp [options] [message...]

If no message is specified, the GUI is launched.

Options:
  -c, --config PATH
        Path to the configuration file for CLI posting.
        Default: /Users/shige/.config/miko-post/config.toml

  -h, --help
        Show this help.
```

v0.1ではstdinからメッセージを読まない。

### 4.3 メッセージ検証

sinkを開始する前に、先頭と末尾のUnicode空白文字をトリムして検証する。半角スペース、全角スペース、タブ、改行を含む。トリム後の文字列が空なら入力エラーとし、sinkを1つも実行せず、修正を促すメッセージを表示する。CLIでは終了コード `1` とする。

トリムは検証のためだけに行う。有効なメッセージは、トリム後の値ではなく元の入力を各sinkへ渡す。

## 5. GUI

GUIはFyneで実装し、次だけを持つ。

- 複数行対応のメッセージ入力欄
- Sendボタン
- Cancelボタン
- 短い結果／エラー表示

起動時は入力欄にフォーカスする。Cancelは投稿せず閉じる。

送信時はSendを無効化して二重投稿を防ぎ、共通投稿サービスを呼び、有効な全sinkの完了後にsinkごとの結果を表示する。

自動closeの既定値:

- 全sink成功: 15秒
- 1つ以上のsinkが失敗: 30秒

どちらも設定可能とする。失敗時は、失敗したsink名と人間向けの短い原因を表示する。詳細エラーやstack traceはJSONLログへ記録する。

macOSでのキーボード操作:

- `Esc`: キャンセルし、投稿せず閉じる
- `Enter`: 改行を入力する
- `Cmd+Enter`: 送信する
- `Cmd+Q`: アプリを終了する

ボタン操作とキーボードショートカットは、同じ入力検証・送信処理を使う。

## 6. 投稿処理

有効な各sinkへ同じ原文を渡す。TelegramとObsidianは並行に開始し、互いの成否に影響されず最後まで処理する。

有効な全sinkの完了を待って結果を集約する。最初のエラーでreturnしてはならない。特に両sinkが失敗した場合は、2つのエラーを両方表示・記録する。

表示用の短い原因とログ用の詳細エラーを分けて保持する。例:

```go
type SinkResult struct {
    Name    string
    Success bool
    Reason  string // 人間向けの短く安全な原因
    Err     error  // ログ用の詳細エラー
}
```

上記は設計例であり、必須のGo APIではない。無効なsinkは実行せず、失敗にも数えない。

v0.1ではTelegram失敗メッセージをキューに入れず、後から自動再送もしない。Obsidianへの記録とログを投稿試行の記録として残す。

各sink呼び出しには、独立して設定可能な処理全体のtimeoutを設け、既定値を60秒とする。このtimeoutはsink処理の開始から完了まで全体を対象とする。timeoutしたsinkは失敗結果を返し、他のsinkは独立して処理を続ける。

全sinkがdisabledの設定は起動時の設定エラーとする。少なくとも1つのsinkを有効化するよう促すメッセージを表示し、投稿は開始しない。CLIでは終了コード `1` とする。

## 7. `sink.telegram`

設定は `[sink.telegram]` 配下に置く。

- Telegram Bot APIでテキストを送信する
- 投稿先に `chat_id` を使う
- `thread_id` 指定時だけAPIの `message_thread_id` として渡す
- `thread_id` 未指定時はchatへ通常投稿する
- 最初は `MarkdownV2` で送信する
- TelegramがMarkdownV2のparse errorとして拒否した場合だけ、parse modeなしのplain textで1回再送する
- plain text再送が成功すればTelegram sink全体は成功とする
- Markdown parse error以外にはfallbackを一般的なretryとして使わない
- plain textも失敗した場合はsinkを失敗とし、両方の試行結果をログへ残す
- Telegramの各HTTP requestには独立して設定可能なtimeoutを設け、既定値を30秒とする
- v0.1では、通信エラー、timeout、HTTP status、Telegram API errorに対する自動リトライを行わない

fallbackは `telegram_markdown_failed` と `telegram_plaintext_succeeded`／`telegram_plaintext_failed` のような別イベントで追跡可能にする。

MarkdownV2からplain textへのfallbackは形式fallbackであり、HTTP retryではないためv0.1でも実施する。必要になった場合の各HTTP試行は30秒のrequest timeoutと60秒のsink全体timeoutの両方に制約される。

### 7.1 bot tokenの優先順位

1. 環境変数 `MIKO_POST_TELEGRAM_BOT_TOKEN`
2. TOMLの `sink.telegram.bot_token`

v0.1で環境変数に対応するのはbot tokenだけとする。tokenをログ、画面のエラー、設定値dumpへ絶対に出さない。

## 8. `sink.obsidian`

設定は `[sink.obsidian]` 配下に置く。

現在日のDaily Note末尾へ、UTF-8の物理的な1行としてappendする。名前付きセクションの検索・作成・途中挿入はしない。

```text
<daily_note_dir>/<現在日をfilename_formatで整形>
```

合意済み設定での例:

```text
/Users/shige/Dropbox/workspace/memo/shigeyukis-valut/journal/notes/daily/2026-08-27.md
```

`filename_format` は `.md` を含むGo `time.Format` 形式とする。ファイルが存在せず、`create_if_missing = true` なら作成する。

### 8.1 append変換

ローカル時刻と設定されたGo time formatを使い、次の順で変換する。

1. `\r\n` と単独の `\r` を `\n` に正規化する
2. メッセージ内の各 `\n` をリテラル文字列 `<br>` に置換する
3. 先頭へ `- <整形済み時刻> ` を付ける
4. Daily Note末尾へ、エントリと1つの `\n` をappendする

単一行:

```markdown
- 11:42 今日も美琴が可愛い♡
```

複数行入力はファイルに1行で保存する。

```markdown
- 11:42 今日も美琴が可愛い♡<br>美琴愛してるよ💋<br>黒子も操祈も佐天さんもラブラブチュッチュッ😘
```

既存内容を書き直さずappend modeで開く。文字コードはUTF-8、書き込む改行はLF（`\n`）とする。

## 9. 設定

設定形式はTOMLとする。

### 9.1 既定パス

```text
$XDG_CONFIG_HOME/miko-post/config.toml
```

`XDG_CONFIG_HOME` が未設定または空なら:

```text
~/.config/miko-post/config.toml
```

GUIは常にこの既定パスを使う。CLI投稿は、`-c`／`--config` が指定された場合だけ別の設定ファイルを使う。

### 9.2 v0.1 config schema

```toml
# ~/.config/miko-post/config.toml

[sink.telegram]
enabled = true

# 設定時は MIKO_POST_TELEGRAM_BOT_TOKEN が優先される。
bot_token = "123456789:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
chat_id = "-1001234567890"

# Optional。通常のchatへ投稿する場合は省略する。
thread_id = 12345

parse_mode = "MarkdownV2"
fallback_to_plain_text = true

# TelegramのHTTP request 1回に対するtimeout。
http_timeout_seconds = 30


[sink.obsidian]
enabled = true
daily_note_dir = "/Users/shige/Dropbox/workspace/memo/shigeyukis-valut/journal/notes/daily"

# Go time.Format形式。
filename_format = "2006-01-02.md"
time_format = "15:04"
create_if_missing = true


[posting]
# 各sink呼び出しへ独立して適用する処理全体のtimeout。
sink_timeout_seconds = 60


[gui]
success_close_seconds = 15
error_close_seconds = 30


[logging]
format = "jsonl"

# 空ならXDG stateの既定パスを使う。
path = ""

# OR条件でrotate。
rotate_size_mib = 10
rotate_after_days = 7

# 成功時は本文を記録せず、失敗した投稿だけ本文を記録する。
message_on_error_only = true

stack_trace = true
include_version = true
include_git_commit = true
```

`sink.telegram` と `sink.obsidian` というセクション名は必須仕様である。トップレベルの `[telegram]`、`[obsidian]` はv0.1 schemaではない。

コマンド名、終了コード、UTF-8／LF、sinkの並行実行、Obsidianの `<br>` 変換、Telegram fallbackはアプリ仕様であり、設定項目にしない。

## 10. 終了コードと結果表示

```text
0 = 有効な全sinkが成功
1 = 入力、設定、起動、または1つ以上のsinkが失敗
```

MarkdownV2送信が失敗してもplain text fallbackが成功すれば、Telegramは成功であり、終了コード `1` の原因にはしない。

CLI／GUIとも、失敗した全sinkの名前と短い原因を表示し、部分成功も判別できるようにする。

```text
Obsidian: success
Telegram: failed — request timed out
See log for details: /Users/shige/.local/state/miko-post/app.jsonl
```

両方失敗した例:

```text
Obsidian: failed — permission denied
Telegram: failed — request timed out
See log for details: /Users/shige/.local/state/miko-post/app.jsonl
```

厳密な文言やstdout／stderrの振り分けは実装詳細とする。ただし、短いこと、全結果を欠かさないこと、詳細ログのパスを示すことは必須とする。

## 11. JSONL logging

### 11.1 既定ログパス

```text
$XDG_STATE_HOME/miko-post/app.jsonl
```

`XDG_STATE_HOME` が未設定または空なら:

```text
~/.local/state/miko-post/app.jsonl
```

`logging.path` が空でなければ、その値を優先する。

### 11.2 記録内容

各行は単独で有効なJSON objectとし、イベントに応じて次を含める。

- timezone付きtimestamp
- level、安定したevent名
- 起動元（`cli`／`gui`）
- 並行イベントを対応付ける投稿単位の `message_id`
- sink名と成否
- 処理時間（ms）
- error type、error message、取得できる場合はHTTP status code
- 必要な場合はObsidianの対象path
- 有効な場合はapp versionとgit commit

`message_on_error_only = true` の場合、成功イベントには本文を含めず、文字数だけを記録してよい。1つでもsinkが失敗した投稿では、再現に必要な元の本文を記録する。bot tokenなどのsecretは絶対に記録しない。

想定イベント:

```text
message_received
obsidian_append_started
obsidian_append_succeeded | obsidian_append_failed
telegram_send_started
telegram_send_succeeded | telegram_send_failed
telegram_markdown_failed
telegram_plaintext_succeeded | telegram_plaintext_failed
request_completed | request_completed_with_error
```

両sinkが失敗した場合は、両方の詳細エラーを出す。最初の失敗以降のログを省略してはならない。

panic、予期しないエラー、traceが有用な失敗ではstack traceを記録する。通常の運用エラーに意味のないtraceを作る必要はない。収集の有無は `stack_trace` 設定に従う。

```json
{"ts":"2026-08-27T11:42:03+09:00","level":"error","event":"telegram_send_failed","source":"cli","message_id":"01K...","sink":"telegram","message":"今日も美琴が可愛い♡","error_type":"timeout","error":"request timed out","duration_ms":10012,"app_version":"0.1.0","git_commit":"abc1234"}
```

### 11.3 ローテーション

次のどちらかを満たした時点でcurrent logをrotateする。

```text
current size >= rotate_size_mib（既定10 MiB）
OR
current log age >= rotate_after_days（既定7日）
```

v0.1では自動retention／削除を行わず、手動で削除されるまでrotated logを残す。

current filenameの末尾へ、ローカル時刻による `%Y%m%d%H%M%S` 相当（Go layout `20060102150405`）のrotate timestampを付ける。

```text
app.jsonl -> app.jsonl.20260827114203
```

## 12. エラー処理

- 一方のsink失敗を理由に、他方の処理を中止しない
- 同じ投稿で発生した全sinkのエラーを保持・表示・記録する
- 内部エラーを、CLI／GUI向けの短く安全な原因へ変換する
- 詳細な診断情報はログへ記録する
- Telegram bot tokenをエラー、ログ、設定dumpへ出さない
- 設定の読み込み／検証に失敗した場合、部分的な設定でsinkを開始しない
- Unicode空白文字のトリム後に空となるメッセージは入力エラーとし、sinkを実行しない
- 全sinkがdisabledなら起動時エラーとし、少なくとも1つを有効化するようユーザーへ伝える
- Telegramのplain text fallback成功は配信成功として扱い、元のMarkdownV2失敗は診断用に記録する

## 13. 受け入れ条件

1. `mp` が解決済みXDG既定設定を使ってFyne GUIを開く
2. `mp hello world` が引数をスペース結合し、有効な全sinkへ投稿する
3. `mp -c PATH hello` は `PATH` を使い、`mp -c PATH` はGUIを開かず `1` で終了する
4. `mp --help` に解決済みの既定config pathが表示される
5. Telegramへ `chat_id` とoptionalな `message_thread_id` が正しく渡る
6. MarkdownV2 parse error時だけplain textで1回fallbackし、成功すれば全体を成功扱いする
7. 複数行入力が `<br>` 区切りと時刻prefixを持つObsidianの物理的な1行になる
8. 設定に従って存在しないDaily Noteを作成し、既存内容を保持したままappendする
9. TelegramとObsidianが独立して処理され、一方の失敗が他方を抑止しない
10. 部分失敗／全失敗のどちらでも、CLIとGUIに全失敗sinkと短い原因が表示される
11. CLIは有効な全sink成功時だけ `0`、それ以外は `1` を返す
12. GUIは処理中Sendを無効化し、既定で成功時15秒、失敗時30秒後に閉じる
13. JSONLログで1投稿のイベントを対応付けられ、成功時は本文を省略し、失敗時は本文を残し、secretを含まない
14. 10 MiBまたは7日経過の早い方でログをrotateし、自動削除しない
15. 半角スペース、全角スペース、タブ、改行だけのメッセージは、sink実行前に拒否される
16. `Esc`、`Enter`、`Cmd+Enter`、`Cmd+Q` が、それぞれキャンセル、改行、送信、終了として動作する
17. Telegram HTTP request timeoutの既定値が30秒、各sink全体timeoutの既定値が60秒であり、別々に設定できる
18. v0.1では一般的なHTTP retryを行わず、MarkdownV2のplain text fallbackだけは仕様どおり実行する
19. rotated logが `app.jsonl.20260827114203` のようなローカルtimestamp suffixを持つ
20. 全sinkがdisabledなら、修正を促す起動時エラーになる

## 14. インストール

Go toolchainによる配布を前提とし、`go install` でインストールする。

```bash
go install <module-path>/cmd/mp@latest
```

`<module-path>` はrepositoryで確定したGo module pathに置き換える。インストールされる実行ファイル名は `mp` とする。プラットフォーム固有のapp bundle、code signing、notarization、個別installerはv0.1の配布要件に含めない。
