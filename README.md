# tmuxui

**PCで tmux + Claude Code エージェントチームによる開発中に、外出先のスマートフォンから tmux セッションを監視・操作できる Webアプリケーション。**

---

## 📱 tmuxui とは

`tmuxui` はローカルホストで起動するシンプルな Web サーバー。スマートフォンのブラウザからアクセスすると、デスク環境で実行中の Claude Code エージェントチームの tmux ペイン内容を閲覧し、権限許可やコマンド送信ができます。

---

## 必要なもの

- **Go 1.22 以上**
- **tmux**（ローカルにインストール済みで、**セッションが起動中**であること）

### tmux のインストール

tmux がまだインストールされていない場合：

```bash
# macOS
brew install tmux

# Ubuntu/Debian
apt install tmux

# その他
# 公式リポジトリまたはパッケージマネージャーから tmux をインストール
```

---

## インストール・ビルド

### 方法 1: go install でインストール（推奨）

```bash
go install github.com/BambooTuna/tmuxui@latest
```

その後 `tmuxui` コマンドで起動できます。

### 方法 2: GitHub Releases からダウンロード

```bash
# Linux/macOS の例
curl -L https://github.com/BambooTuna/tmuxui/releases/download/v0.1.0/tmuxui_0.1.0_darwin_amd64.tar.gz -o tmuxui.tar.gz
tar xzf tmuxui.tar.gz
./tmuxui
```

### 方法 3: ソースからビルド

```bash
# リポジトリをクローン
git clone https://github.com/BambooTuna/tmuxui.git
cd tmuxui

# ビルド
go build -o tmuxui .

# 実行可能ファイルが生成される
./tmuxui
```

---

## 🚀 起動方法

### 基本的な起動

```bash
./tmuxui
```

起動時に以下が表示されます：

```
tmuxui v0.1.0
Listening on http://127.0.0.1:6062
Access URL: http://127.0.0.1:6062?token=a3f8b2c1d4e5f6a7
```

このURLをスマートフォンにコピーしてブラウザで開きます。

### ポート変更

デフォルトポート (6062) が既に使用中の場合は、`--port` オプションで変更できます：

```bash
./tmuxui --port 3000
```

### 外部からのアクセス（ポートフォワード用）

```bash
./tmuxui --host 0.0.0.0 --port 6062
```

⚠️ **セキュリティ上の注意**: 外部公開時は必ず `--token` で認証トークンを指定するか、ファイアウォール・SSH ポートフォワード経由でアクセスしてください。

### 認証トークンの指定

```bash
./tmuxui --token mytoken
```

デフォルトは起動時にランダムに生成されます。

### 開発モード

```bash
./tmuxui --dev
```

このモードでは HTML/CSS/JavaScript をファイルシステムから直接読み込みます。開発時の変更がブラウザリロードで即座に反映されます（Go の再ビルド不要）。

---

## 📲 スマートフォンからのアクセス

### 方法 1: 同一ネットワーク内

1. PC で外部からのアクセスを許可して起動：

```bash
./tmuxui --host 0.0.0.0 --port 6062
```

2. PC のIPアドレスを確認：

```bash
# macOS/Linux
ifconfig

# または
ip addr
```

3. PC と同じ WiFi にスマートフォンが接続
4. 起動時に表示されたURL（例: `http://192.168.1.100:6062?token=...`）をスマートフォンのブラウザで開く

### 方法 2: SSH ポートフォワード

外出先など異なるネットワーク環境では、SSH ポートフォワードを使用：

```bash
# スマートフォン側で以下を実行（例: iOS Termius App）
ssh -L 6062:localhost:6062 user@your-pc-hostname

# または PC 側で以下を実行してから、スマートフォンで http://localhost:6062?token=... にアクセス
ssh -R 6062:localhost:6062 user@your-pc-hostname
```

### 方法 3: Tailscale / VPN

Tailscale や WireGuard などの VPN を使用して PC と スマートフォンを同じネットワークに接続する方法もあります。

---

## ✨ できること

- **tmux セッション・ペイン一覧表示**: エージェント別に階層表示
- **ペイン内容のリアルタイム閲覧**: ターミナル出力を整形表示
- **権限許可リクエストへの応答**: Claude Code の権限許可をスマートフォンから承認・拒否
- **AUTO（自動許可）**: セッション詳細画面の AUTO ボタンで権限許可を自動承認モードに切り替え
- **キー入力送信**: ペインへコマンド・キー入力を送信
- **手動更新**: 🔄 ボタンで ペイン内容を即座に更新
- **ペインリサイズ**: ブラウザの表示サイズに合わせてペインを自動リサイズ
- **セッション管理**: セッションの作成・削除・名前変更
- **スニペット機能**: よく使うコマンドをスニペットとして保存・呼び出し

### スニペット機能の詳細

`snippets/` ディレクトリに JSON ファイルを配置することで、Web UI からスニペットを呼び出せます。

**配置場所**: `tmuxui` 実行ファイルと同じディレクトリに `snippets/` を作成

```
tmuxui
└── snippets/
    ├── build.json
    ├── test.json
    └── deploy.json
```

**ファイル形式**: JSON ファイルで定義（詳細は UI から確認可能）

---

## ❌ できないこと（セキュリティ上の設計）

- ペイン内容の編集・削除
- フルターミナルアクセス

---

## 🔧 CLIオプション一覧

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--port` | 6062 | リッスンポート |
| `--host` | 127.0.0.1 | バインドアドレス |
| `--token` | (自動生成) | 認証トークン。指定しない場合は起動時にランダム生成 |
| `--dev` | false | 開発モード。ファイルシステムから直接読み込み（ブラウザリロードで変更反映） |

### 認証トークンの優先順位

1. `--token` フラグで指定した値
2. `TMUXUI_TOKEN` 環境変数で設定した値
3. 起動時に自動生成されたランダムトークン

```bash
# 環境変数で固定トークンを設定
export TMUXUI_TOKEN=mytoken
./tmuxui
```

### 複合例

```bash
# ポート3000、外部アクセス許可、トークン固定
./tmuxui --host 0.0.0.0 --port 3000 --token mytoken

# 開発モード、ポート3000
./tmuxui --port 3000 --dev
```

---

## 🔒 セキュリティ

- **認証**: 起動時に生成されたランダムトークンを使用。全 HTTP リクエストに `?token=xxx` が必須
- **localhost バインド**: デフォルトは `127.0.0.1` のみでリッスン（外部からアクセス不可）
- **権限許可**: スマートフォンからの権限許可には確認ダイアログが必須
- **セッションタイムアウト**: 10 分無操作でセッション終了

外部公開時は SSH ポートフォワードや VPN 経由での使用を推奨します。

---

## 📚 ドキュメント

詳細な設計・要件は以下を参照：

- **[docs/README.md](./docs/README.md)** - ドキュメント案内（設計・要件書）
- **[docs/requirements.md](./docs/requirements.md)** - 要件定義書
- **[docs/ux-design.md](./docs/ux-design.md)** - UI/UX 詳細設計
- **[docs/architecture.md](./docs/architecture.md)** - システムアーキテクチャ設計
- **[docs/research.md](./docs/research.md)** - 既存ソリューション調査
- **[docs/decisions.md](./docs/decisions.md)** - 技術選定・設計判断ログ

---

## 🛠️ 開発者向け

### プロジェクト構成

```
tmuxui/
├── main.go              # エントリーポイント
├── server.go            # HTTPサーバー
├── handler.go           # REST APIハンドラ
├── websocket.go         # WebSocketハンドラ
├── tmux.go              # tmux ラッパー
├── detector.go          # 権限許可プロンプト検出
├── web/
│   ├── index.html       # SPA
│   ├── app.js           # フロントエンド
│   └── style.css        # スタイル
└── docs/
    ├── architecture.md
    ├── requirements.md
    └── ...
```

### 開発時の起動

```bash
./tmuxui --dev
```

この場合、`web/` 配下の HTML/CSS/JS をファイルシステムから直接配信します。変更後、ブラウザリロードで反映確認できます。

### テスト

```bash
go test ./...
```

---

## 📄 ライセンス

MIT License

詳細は [LICENSE](./LICENSE) を参照してください。

---

## 🙋 サポート

問題報告・質問は GitHub Issues を使用してください。
