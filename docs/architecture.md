# tmuxui システムアーキテクチャ設計

**設計日**: 2026年2月21日
**設計者**: architect（シニアシステムアーキテクト）

---

## 1. 技術スタック選定

### 候補の詳細比較

5つの候補を実環境（macOS arm64、ユーザーのasdf環境）で検証した。

#### Go（net/http + embed + gorilla/websocket）

```
起動: ./tmuxui （単バイナリ）
HTTP: net/http（標準ライブラリ）
WS:   gorilla/websocket（外部依存1個）
埋込: //go:embed web/*（標準ライブラリ、ディレクトリ丸ごと埋め込み）
サイズ: ~10-15MB
```

- `embed`パッケージはGo 1.16+標準。`//go:embed web/*`一行でweb/配下の全ファイルをバイナリに埋め込み
- `net/http`でHTTPサーバーが完結。フレームワーク不要
- gorilla/websocketはアーカイブ済みだが極めて安定。事実上のGoのWS標準
- `go build`一発で完全自己完結バイナリ。クロスコンパイルも`GOOS=linux go build`
- 開発時は`-tags dev`フラグでファイルシステムから直接読み込むモードに切替可能

#### Bun（Bun.serve() 単体、フレームワークなし）

```
起動: bun run server.ts  or  ./tmuxui（compile後）
HTTP: Bun.serve()（ランタイム内蔵）
WS:   Bun.serve() websocket（ランタイム内蔵）
埋込: import ... with { type: "file" }（ファイル個別指定）
サイズ: ~57MB（compile時、実測）
```

- **外部依存ゼロ**。HTTP + WebSocketがBunランタイムに内蔵
- TypeScript一本でフロント・バック統一記述。1言語で完結
- `bun --hot server.ts`でホットリロード。開発サイクルが最速
- `bun build --compile server.ts --outfile tmuxui`で単バイナリ化可能
- ただし静的ファイル埋め込みは`import path from "./file" with { type: "file" }`で**ファイル個別指定が必要**（Goのディレクトリ丸ごと埋め込みと違い、ファイル追加のたびにimport文追加）
- compile後バイナリは57MB（Bunランタイムをバンドルするため）
- 実測: `Bun.file(import.meta.dir + "/web/...")`方式はcompile時に埋め込まれない（ENOENT）。`import with { type: "file" }`が必須

#### Bun + Hono

```
起動: bun run server.ts
HTTP: Hono（~14KB）
WS:   Bun内蔵 + Hono adapter
埋込: Bun.serve()と同じ
外部依存: hono 1個
```

- Honoは超軽量Webフレームワーク。ルーティング・ミドルウェアが整理される
- ただし本プロジェクトのAPIは4エンドポイント程度。Honoのルーター機能は過剰
- Bun.serve()のfetchハンドラ内でURLパターンマッチするだけで十分な規模
- **Honoを入れる理由が薄い**。依存ゼロのBun.serve()単体で足りる

#### Rust + Axum（先行版の推奨案）

- 外部依存20個以上（axum, tokio, serde, serde_json, tokio-tungstenite, anyhow, tracing...）
- コンパイル時間が長い（初回ビルド数分）
- 個人ツールMVPにRustの安全性保証は不要
- **却下**

#### Python / Node.js

- 単バイナリ化が困難（PyInstaller、pkg等は不安定）
- 依存管理が重い（pip, node_modules）
- **却下**

### 比較表（実測ベース）

| 項目 | **Go** | **Bun** | Bun+Hono | Rust |
|------|:---:|:---:|:---:|:---:|
| 起動コマンド | `./tmuxui` | `bun run server.ts` | 同左 | `./tmuxui` |
| バイナリサイズ | ~10-15MB | ~57MB | ~57MB | ~15-20MB |
| 外部依存数 | **1個** | **0個** | 1個 | 20個以上 |
| WebSocket | gorilla/websocket | 内蔵 | 内蔵 | tokio-tungstenite |
| 静的ファイル埋込 | `//go:embed web/*` | ファイル個別import | 同左 | include_str! |
| 言語統一 | Go + JS | TS一本 | TS一本 | Rust + JS |
| 開発時リロード | 再ビルド必要 | ホットリロード | 同左 | 再ビルド必要 |
| コンパイル速度 | ~2秒 | ~0.2秒 | ~0.2秒 | 数分 |
| ランタイム成熟度 | 非常に高い | v1.3（発展途上） | 同左 | 非常に高い |
| 配布しやすさ | バイナリコピー | Bun必要 or compile | 同左 | バイナリコピー |

### 選定結果: **Go**

**決定理由:**

1. **静的ファイル埋め込みの信頼性**: Goの`//go:embed web/*`はディレクトリ丸ごと一行で埋め込める。Bunは`import with { type: "file" }`でファイル個別指定が必要で、ファイル追加のたびにimport文を足す必要がある。3ファイル程度なら問題ないが、Goの方がクリーン
2. **真の自己完結バイナリ**: Go binary（10-15MB）はOS標準ライブラリ以外に依存なし。Bun compile（57MB）はBunランタイムをバンドルする形であり、バイナリサイズが4倍以上
3. **安定性**: `net/http`は15年以上の実績。Bunは急速に成熟しているがv1.3。個人ツールは「一度作って長く使う」用途なので、枯れた技術の方が保守コストが低い
4. **開発速度の懸念は解消可能**: Go+embedの「フロント変更で再ビルド」問題は、devモードフラグでファイルシステムから直接読み込みにすれば解決（後述）

**Bunが上回る点（トレードオフの認識）:**
- 外部依存ゼロ（Go: 1個）
- TypeScript一本の言語統一
- ホットリロードの開発体験
- ただし、フロントエンド3ファイル・APIエンドポイント4個のこの規模では、これらのメリットの実効的な差は小さい

---

## 2. フロントエンド構築方法

### 選択肢の検討

| 方式 | 概要 | 判定 |
|------|------|:---:|
| **静的HTML+JS+CSS配信** | web/配下に3ファイル。embedでバイナリ埋め込み | **採用** |
| Hono JSX (SSR) | サーバーサイドレンダリング | 却下（Go採用のため対象外） |
| htmx | HTMLフラグメント返却+差分更新 | 却下（WebSocketとの相性が悪い） |
| 単一HTMLファイル（全インライン） | CSS/JSを全てHTML内に記述 | 却下（開発時の編集性が悪い） |

### 採用方式: 静的ファイル配信（embed.FS）

```
web/
├── index.html       # メインHTML（SPA）
├── app.js           # フロントエンドロジック（vanilla JS）
└── style.css        # スタイル（ダークモード）
```

**Go側の配信コード:**

```go
//go:embed web/*
var webFS embed.FS

func setupRoutes(mux *http.ServeMux) {
    // 静的ファイル配信
    mux.Handle("/", http.FileServer(http.FS(webFS)))

    // API
    mux.HandleFunc("/api/sessions", handleSessions)
    mux.HandleFunc("/api/panes/", handlePanes)
    mux.HandleFunc("/ws", handleWebSocket)
}
```

**devモード（開発時のリロード対応）:**

```go
// -dev フラグ指定時はファイルシステムから直接読み込み
// HTMLやJSの変更がブラウザリロードだけで反映される
if devMode {
    mux.Handle("/", http.FileServer(http.Dir("web/")))
} else {
    mux.Handle("/", http.FileServer(http.FS(webFS)))
}
```

### フロントエンドの実装方針

- **フレームワークなし**（vanilla JavaScript）。画面数3つ・コンポーネント数5つ程度の規模にReact/Vue/Preactは過剰
- **ビルドステップなし**。TypeScriptも不要（この規模でTS型安全性のメリットは薄い）
- **CDN依存なし**。全アセットをバイナリに埋め込み
- **SPA構成**: index.htmlにJSで画面遷移を実装。History APIは不使用（hash routingで十分）
- WebSocket接続管理、ペイン内容表示、権限許可ダイアログをapp.jsに記述

### フロントエンドの画面構成

```
index.html
├── #pane-list     ← ペイン一覧画面（デフォルト表示）
├── #pane-detail   ← ペイン詳細画面（ペイン選択後）
└── #permission    ← 権限許可ダイアログ（フローティング、検出時表示）
```

画面遷移はDOM要素のshow/hideで実装。ルーターライブラリは不要。

---

## 3. 全体構成

```
┌──────────────────────────────────────┐
│          tmuxui (単一バイナリ)         │
│                                      │
│  ┌──────────┐  ┌──────────────────┐  │
│  │  静的     │  │  HTTP/WS サーバー │  │
│  │  ファイル  │  │  (net/http)      │  │
│  │  (embed)  │  │                  │  │
│  └──────────┘  └────────┬─────────┘  │
│                          │            │
│                 ┌────────┴─────────┐  │
│                 │  tmux ブリッジ    │  │
│                 │  (os/exec)       │  │
│                 └────────┬─────────┘  │
│                          │            │
└──────────────────────────┼────────────┘
                           │
                    tmux CLI (ローカル)
                           │
            ┌──────────────┼──────────────┐
            │              │              │
      session:1.1    session:1.2    session:1.3
      (team-lead)    (agent-1)     (agent-2)
```

### コンポーネント

| コンポーネント | 実装 | 役割 |
|---|---|---|
| HTTPサーバー | `net/http` | HTML配信 + REST API |
| WebSocketサーバー | `gorilla/websocket` | リアルタイム双方向通信 |
| tmuxブリッジ | `os/exec` + tmux CLI | セッション・ペイン操作 |
| 静的ファイル | `embed.FS` | HTML/CSS/JSのバイナリ埋め込み |

一体型構成。1プロセス、1バイナリ、外部プロセス依存はtmuxのみ。

---

## 4. tmux連携

### 使用するtmuxコマンド

```bash
# ペイン一覧取得
tmux list-panes -a -F '#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_current_command}\t#{pane_width}\t#{pane_height}\t#{pane_pid}'

# ペイン内容取得（末尾N行）
tmux capture-pane -t "session:window.pane" -p -S -200

# キー入力送信
tmux send-keys -t "session:window.pane" "y" Enter

# セッション一覧
tmux list-sessions -F '#{session_name}\t#{session_windows}\t#{session_attached}'
```

### ポイント

- `list-panes -a` で全セッションの全ペインを一括取得。`-F`でTSV形式にしてパースを単純化
- `capture-pane -p` で標準出力にペイン内容を出力。`-S -200` で最新200行（スマホ表示に十分）
- `send-keys` でキー入力送信。権限許可の応答やコマンド入力に使用
- ANSI制御シーケンスは `capture-pane` のデフォルト（`-e`なし）でストリップされる。テキストのみ取得

### ペイン更新方式

**サーバー側ポーリング（1秒間隔）+ クライアント手動更新の併用**

```
[サーバー]                    [クライアント]
    │                              │
    │── 1秒ごとにcapture-pane ──→  │
    │   変更があればWSでpush        │
    │                              │
    │←── 🔄ボタン押下時 ──────────  │
    │   即座にcapture-pane実行     │
    │   結果をWSで返却             │
```

- サーバーは**アクティブに閲覧中のペインのみ**をポーリング（全ペインを常時監視しない）
- クライアントがWebSocket接続時に「どのペインを見ているか」をsubscribe
- 手動更新ボタンで即座にcapture-pane→最新内容取得

---

## 5. リアルタイム更新

### WebSocket を選定

| 方式 | 双方向 | 遅延 | 判定 |
|------|:---:|:---:|:---:|
| **WebSocket** | Yes | 低 | **採用** |
| SSE | No（サーバー→クライアントのみ） | 低 | 却下（キー入力送信が別途必要） |
| ポーリング | - | 高 | 却下（レスポンス遅い） |

WebSocketなら**ペイン更新のpush + キー入力送信を1本のコネクションで処理**。

### WebSocketメッセージ仕様

**サーバー → クライアント:**

```jsonc
// ペイン内容更新
{ "type": "pane_content", "target": "session:1.2", "content": "...", "ts": 1708500000 }

// ペイン一覧更新
{ "type": "pane_list", "panes": [{ "target": "session:1.1", "cmd": "claude", "size": "139x12" }, ...] }

// 権限許可リクエスト検出
{ "type": "permission_detected", "target": "session:1.2", "prompt": "Allow Bash: npm test ?" }
```

**クライアント → サーバー:**

```jsonc
// ペイン購読（閲覧中のペインを指定）
{ "type": "subscribe", "target": "session:1.2" }

// ペイン購読解除
{ "type": "unsubscribe", "target": "session:1.2" }

// キー入力送信
{ "type": "send_keys", "target": "session:1.2", "keys": "y\n" }

// 手動更新リクエスト
{ "type": "refresh", "target": "session:1.2" }
```

---

## 6. API設計

### エンドポイント一覧

```
GET  /                          → SPA（index.html）
GET  /web/*                     → 静的ファイル（CSS/JS）

GET  /api/sessions              → セッション＆ペイン一覧
GET  /api/panes/:target/content → ペイン内容取得（target例: main:1.0）

POST /api/panes/:target/keys    → キー入力送信
     Body: { "keys": "y\n" }

WS   /ws                        → WebSocket接続
```

### 認証

全エンドポイントに `?token=xxx` クエリパラメータ必須（後述のセキュリティ設計参照）。

### レスポンス例

**GET /api/sessions**
```json
{
  "sessions": [
    {
      "name": "tmuxui-29",
      "windows": 1,
      "attached": true,
      "panes": [
        { "target": "tmuxui-29:1.1", "cmd": "claude", "size": "59x49" },
        { "target": "tmuxui-29:1.2", "cmd": "claude", "size": "139x12" },
        { "target": "tmuxui-29:1.3", "cmd": "claude", "size": "139x12" }
      ]
    }
  ]
}
```

**GET /api/panes/tmuxui-29:1.2/content**
```json
{
  "target": "tmuxui-29:1.2",
  "content": "$ npm test\n\nAll tests passed.\n$ _",
  "lines": 200,
  "ts": 1708500000
}
```

---

## 7. Claude Code 権限許可の検出と応答

### 方式: capture-pane パターンマッチ

Claude Code hooksによるプロセス間連携ではなく、**tmux capture-paneの出力からパターンマッチで権限許可プロンプトを検出する方式**を採用。

理由:
- hooksは外部プロセス連携が複雑で、Claude Codeのバージョン依存が生まれる
- capture-paneは表示されている内容をそのまま取得するだけ。シンプルで壊れにくい
- 個人ツールにおいてはこの方式で十分実用的

### 検出パターン

```go
// Claude Codeの権限許可プロンプトのパターン例
var permissionPatterns = []string{
    "Allow",           // "Allow Bash: ...", "Allow Read: ..." etc.
    "Do you want to",  // 旧形式
}

func detectPermission(content string) (bool, string) {
    lines := strings.Split(content, "\n")
    // 末尾20行を検査（プロンプトは画面下部に表示される）
    start := max(0, len(lines)-20)
    for _, line := range lines[start:] {
        for _, pattern := range permissionPatterns {
            if strings.Contains(line, pattern) {
                return true, line
            }
        }
    }
    return false, ""
}
```

### 応答フロー

```
1. サーバーがcapture-paneで権限許可プロンプトを検出
2. WebSocketで { "type": "permission_detected", ... } をクライアントに通知
3. クライアントが権限許可ダイアログを表示
4. ユーザーが「許可」or「拒否」を選択
5. クライアントが { "type": "send_keys", "keys": "y\n" } を送信
6. サーバーが tmux send-keys で対象ペインにキー送信
```

### 注意事項

- Claude Codeの権限許可UIのフォーマットはバージョンによって変わる可能性がある
- 検出パターンはJSON設定ファイルで変更可能にしておく（ハードコードしない）
- 誤検出のリスクがあるため、**自動応答は行わない**。必ずユーザー確認を経由する

---

## 8. セキュリティ設計

### 方針: 個人ツールとして最小限

**起動時ランダムトークン方式**

```
$ ./tmuxui
tmuxui started on http://localhost:6062?token=a3f8b2c1d4e5
```

1. 起動時に暗号学的に安全なランダムトークン（16バイト hex）を生成
2. トークン付きURLを標準出力に表示
3. ユーザーがこのURLをスマホにコピー（QRコード表示もオプションで対応）
4. 全HTTPリクエスト・WebSocket接続で `?token=xxx` を検証
5. トークン不一致は 403 Forbidden

### localhostバインド

- デフォルトは `127.0.0.1:6062`（ローカルのみ）
- `--host 0.0.0.0` オプションで外部アクセス許可（ポートフォワード経由で使う場合）
- 外部公開時はトークン認証が唯一の防御線（それで十分）

---

## 9. ディレクトリ構成

```
tmuxui/
├── main.go              # エントリーポイント、サーバー起動、CLIオプション
├── server.go            # HTTPルーティング、ミドルウェア
├── handler.go           # REST APIハンドラ
├── websocket.go         # WebSocketハンドラ
├── tmux.go              # tmux CLIラッパー（list-panes, capture-pane, send-keys）
├── detector.go          # 権限許可プロンプト検出
├── web/
│   ├── index.html       # SPA（単一HTMLファイル）
│   ├── app.js           # フロントエンドロジック
│   └── style.css        # スタイル（ダークモード）
├── go.mod
├── go.sum
└── docs/
    ├── architecture.md  # 本ドキュメント
    ├── research.md
    └── ux-design.md
```

- **フラット構成** — ファイル数10未満。パッケージ分割しない（`package main`で十分）
- **web/ 配下は3ファイルのみ** — フレームワーク・ビルドツール不要
- **テストは `_test.go`** — Go標準に従い同ディレクトリに配置

---

## 10. 起動方法

### ビルドと実行

```bash
# ビルド
go build -o tmuxui .

# 実行（デフォルト: localhost:6062）
./tmuxui

# ポート指定
./tmuxui --port 3000

# 外部アクセス許可（ポートフォワード用）
./tmuxui --host 0.0.0.0 --port 6062

# 開発モード（web/からファイル直接読み込み、リロードで反映）
./tmuxui --dev
```

### CLIオプション

| オプション | デフォルト | 説明 |
|---|---|---|
| `--port` | 6062 | リッスンポート |
| `--host` | 127.0.0.1 | バインドアドレス |
| `--token` | (自動生成) | 認証トークン指定（固定したい場合） |
| `--dev` | false | 開発モード（embed使わずファイル直接配信） |

---

## 11. 依存関係

```
module github.com/BambooTuna/tmuxui

go 1.23

require github.com/gorilla/websocket v1.5.3
```

**外部依存: 1個のみ。** Go標準ライブラリで実現できないのはWebSocketだけ。
