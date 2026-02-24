# tmuxui セキュリティレビュー

**レビュー日**: 2026年2月21日
**レビュアー**: security-reviewer
**対象バージョン**: v0.1.0

---

## 前提

- **個人ツール**。ローカルネットワーク or ポートフォワード経由でのみアクセスされる
- 評価はリスクと修正コストのバランスで判断
- 深刻度: 問題なし / 低リスク / 中リスク / 高リスク

---

## 1. コマンドインジェクション

**評価: 問題なし**

### 分析

`tmux.go` の `sendKeys`、`capturePane`、`listSessions` はすべて `exec.Command("tmux", args...)` を使用している。Go の `exec.Command` はシェルを介さず直接プロセスを起動するため、`;`、`|`、`&&` などのシェルメタ文字によるコマンドインジェクションは **原理的に不可能**。

```go
// tmux.go:110 - args はシェルを通らず直接 tmux プロセスに渡される
func sendKeys(target, keys string) error {
    args := []string{"send-keys", "-t", target}
    // ...
    return exec.Command("tmux", args...).Run()
}
```

`target` パラメータは `-t` フラグの引数として渡されるため、仮に `-l` のようなフラグ風文字列が来ても、tmux は `-t` の引数（ターゲット名）として解釈する。フラグインジェクションも成立しない。

`keys` は tmux send-keys の引数として渡される。tmux が解釈するキー名（`Enter`、`C-c` 等）を送信できるが、これは**意図された機能**。

### 結論

シェルインジェクションの経路なし。追加対策不要。

---

## 2. XSS（クロスサイトスクリプティング）

**評価: 問題なし**

### 分析

#### innerHTML を使用する箇所（app.js）

`renderSessions()` 内の2箇所で innerHTML を使用：

```javascript
// app.js:223-226 - セッションヘッダー
hdr.innerHTML =
    `<span class="session-toggle">${collapsed ? '▶' : '▼'}</span>` +
    `<span class="session-name">${esc(session.name)}</span>` +
    `<span class="session-meta">${session.panes.length} panes</span>`;

// app.js:235-238 - ペインアイテム
btn.innerHTML =
    `<span class="pane-cmd">${esc(pane.cmd)}</span>` +
    `<span class="pane-id">${esc(paneId)}</span>` +
    `<span class="pane-size">${esc(pane.size || '')}</span>`;
```

すべての動的値に `esc()` 関数を通している。

#### esc() 関数の安全性（app.js:414-420）

```javascript
function esc(str) {
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}
```

HTML タグ挿入（`<script>`等）と属性値エスケープ（`"` breakout）を防止している。シングルクォート（`'`）のエスケープがないが、テンプレート内の HTML 属性はすべてダブルクォートを使用しているため問題ない。

#### textContent を使用する箇所（安全）

ペイン内容やダイアログの動的コンテンツはすべて `textContent` で設定：

| 箇所 | コード | 安全性 |
|------|--------|--------|
| ペイン内容表示 | `el.textContent = content` (197行) | 安全 |
| ペインタイトル | `$('pane-title').textContent = ...` (178行) | 安全 |
| 権限プロンプト | `$('permission-prompt').textContent = ...` (287行) | 安全 |
| 確認ダイアログ | `$('confirm-content').textContent = text` (313行) | 安全 |

`textContent` は HTML をパースしないため XSS リスクなし。

### 結論

innerHTML 使用箇所はすべて適切にエスケープされ、動的コンテンツ表示は textContent を使用。XSS の経路なし。

---

## 3. 認証の堅牢性

**評価: 問題なし**

### トークン生成（main.go:18-24）

```go
b := make([]byte, 16)
if _, err := rand.Read(b); err != nil {
    log.Fatal(err)
}
*token = hex.EncodeToString(b)
```

- `crypto/rand` 使用（暗号学的に安全な乱数）
- 16バイト = **128ビットのエントロピー** → ブルートフォース不可能
- hex エンコードで32文字のトークン

### 認証ミドルウェア（server.go:33-41）

```go
func authMiddleware(validToken string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Query().Get("token") != validToken {
            http.Error(w, "Forbidden", http.StatusForbidden)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

- 全エンドポイント（REST API、WebSocket、静的ファイル）にトークン検証が適用されている
- WebSocket 接続も HTTP アップグレード前にミドルウェアを通過する

### タイミング攻撃耐性

Go の `!=` 演算子は定数時間比較ではない。理論上タイミング攻撃が成立し得る。しかし：

- ネットワーク遅延のノイズ >> 文字列比較の時間差
- localhost / ポートフォワード環境ではネットワークジッターが支配的
- 128ビットエントロピーのトークンに対するタイミング攻撃は非現実的

個人ツールとしては **対策不要**。

### トークンが URL クエリパラメータにある点

トークンが URL に含まれるため、ブラウザ履歴に残る。しかし：

- 個人端末のブラウザ履歴であり、リスクは許容範囲
- 外部リンクがアプリ内にないため、Referer ヘッダー経由のリークもない
- サーバーログへの記録は `log.Fatal(http.ListenAndServe(...))` のみで、リクエストログは出力されていない

### 結論

個人ツールとして十分な認証強度。追加対策不要。

---

## 4. CORS / Origin 検証

**評価: 低リスク**

### 現状（websocket.go:12-14）

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}
```

`CheckOrigin` が常に `true` を返すため、任意のオリジンからの WebSocket 接続を許可している。

### 攻撃シナリオ

1. ユーザーがブラウザで `http://evil.com` にアクセス
2. `evil.com` の JavaScript が `ws://localhost:6062/ws?token=xxx` に接続を試みる
3. **ただし、トークンを知らなければ接続は403で拒否される**

トークンが漏洩していない限り攻撃は成立しない。トークンはURLに含まれるため、理論上はブラウザ拡張機能や履歴から漏洩する可能性があるが、個人ツールにおいては現実的な脅威ではない。

### 修正推奨

個人ツールとして対策不要。もし追加の防御層を入れたい場合は以下の通り：

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        origin := r.Header.Get("Origin")
        // Origin ヘッダーがない（非ブラウザクライアント）か、同一ホストなら許可
        return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
    },
}
```

**修正は任意**。トークン認証が主防御線であり、CheckOrigin は二重防御。

---

## 5. 入力検証

**評価: 低リスク**

### target パラメータ

`handler.go` と `websocket.go` で `target` のフォーマット検証がない。

```go
// handler.go:20 - バリデーションなし
target, _ := url.PathUnescape(r.PathValue("target"))
pc, err := capturePane(target)
```

不正な target は tmux が単にエラーを返すだけなので、セキュリティ上の実害はない。

### keys の長さ制限

クライアント側は `maxlength="256"`（index.html:33）だが、サーバー側に長さ制限がない。

```go
// handler.go:32-38 - サイズ制限なし
var body struct {
    Keys string `json:"keys"`
}
if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
```

`json.NewDecoder` はデフォルトで無制限に読み込む。理論上、巨大な JSON を送りつけてメモリを消費させることが可能。

### 修正推奨

リクエストボディのサイズ制限を追加する（任意）：

```go
// handler.go:handlePaneKeys に追加
func handlePaneKeys(w http.ResponseWriter, r *http.Request) {
    target, _ := url.PathUnescape(r.PathValue("target"))
    r.Body = http.MaxBytesReader(w, r.Body, 1024) // 1KB 制限
    var body struct {
        Keys string `json:"keys"`
    }
    // ...
}
```

**修正は任意**。認証済みユーザー（= 自分自身）しかアクセスできないため、実害の可能性は極めて低い。

---

## 6. 情報漏洩

**評価: 低リスク**

### エラーメッセージ（handler.go）

```go
// handler.go:12
http.Error(w, err.Error(), http.StatusInternalServerError)

// handler.go:23
http.Error(w, err.Error(), http.StatusInternalServerError)

// handler.go:40
http.Error(w, err.Error(), http.StatusInternalServerError)
```

tmux コマンドのエラーメッセージがそのままクライアントに返される。メッセージには以下が含まれ得る：

- tmux のバージョン情報
- セッション名やウィンドウ名
- `exit status` などのシステム情報

### 結論

認証済みユーザー（= 自分自身）にのみ返されるため、情報漏洩の実害なし。**対策不要**。

---

## 7. DoS（サービス拒否）

**評価: 問題なし**

### WebSocket 接続数

接続数に上限がないが、認証トークンが必要なため、攻撃者が大量接続を確立することは困難。

### ポーリング頻度

```go
// websocket.go:61 - サーバー側で1秒固定
ticker := time.NewTicker(time.Second)
```

ポーリング頻度はサーバー側で固定されており、クライアント数に依存しない（同一ターゲットの複数クライアントに対して1回の capture-pane で処理）。

### バッファリング

```go
// websocket.go:160
send: make(chan []byte, 64),

// websocket.go:146-148 - 遅延クライアントはメッセージをドロップ
select {
case c.send <- msg:
default:
}
```

遅いクライアントに対してはメッセージをドロップする設計。バックプレッシャーが適切に処理されている。

### 結論

個人ツールとして十分な設計。対策不要。

---

## 総合評価

| # | 観点 | 評価 | 対応 |
|---|------|------|------|
| 1 | コマンドインジェクション | **問題なし** | 不要 |
| 2 | XSS | **問題なし** | 不要 |
| 3 | 認証の堅牢性 | **問題なし** | 不要 |
| 4 | CORS/Origin | **低リスク** | 任意 |
| 5 | 入力検証 | **低リスク** | 任意 |
| 6 | 情報漏洩 | **低リスク** | 不要 |
| 7 | DoS | **問題なし** | 不要 |

### 総括

**高リスク・中リスクの問題はゼロ**。個人ツールとして適切なセキュリティレベルを達成している。

主要な防御ラインであるトークン認証は、128ビットエントロピーの暗号学的乱数で生成され、全エンドポイントに適用されている。Go の `exec.Command` によりシェルインジェクションは原理的に防止され、フロントエンドの XSS 対策も `textContent` と `esc()` 関数で適切に実装されている。

低リスクとして指摘した CORS/Origin と入力検証の2点は、いずれも「トークンを知らない攻撃者には到達不能」という前提の上での理論的リスクであり、修正は任意。個人ツールの使用形態を考慮すると、**現状のまま運用して問題ない**。
