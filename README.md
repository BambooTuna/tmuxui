# tmuxui

[![Release](https://img.shields.io/github/release/BambooTuna/tmuxui.svg)](https://github.com/BambooTuna/tmuxui/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**PCで tmux + Claude Code エージェントチームによる開発中に、外出先のスマートフォンから tmux セッションを監視・操作できる Webアプリケーション。**

---

## 📱 tmuxui とは

`tmuxui` はローカルホストで起動するシンプルな Web サーバー。スマートフォンのブラウザからアクセスすると、デスク環境で実行中の Claude Code エージェントチームの tmux ペイン内容を閲覧し、権限許可やコマンド送信ができます。

---

## 必要なもの

- **Go 1.24 以上**
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

### 方法 1: go install でインストール

Go がインストール済みの方向けです。

```bash
go install github.com/BambooTuna/tmuxui@latest
```

インストール後、動作確認します：

```bash
tmuxui --help
```

> **`command not found` になる場合**: Go のバイナリ配置先が PATH に含まれていない可能性があります。以下をシェルの設定ファイルに追加してください：
>
> ```bash
> # ~/.zshrc または ~/.bashrc に追記
> export PATH="$(go env GOPATH)/bin:$PATH"
> ```
>
> 追加後、ターミナルを再起動するか `source ~/.zshrc`（bash の場合は `source ~/.bashrc`）を実行してください。

### 方法 2: GitHub Releases からダウンロード（Go 不要）

Go をインストールしていない場合はこちらの方法が便利です。

#### ステップ 1: 自分の環境を確認

まず、ターミナルで以下を実行して自分の環境を確認します：

```bash
uname -ms
```

表示結果を見て、以下のどれに当てはまるか確認してください：

| 表示結果 | 環境 |
|---------|------|
| `Darwin arm64` | macOS（Apple Silicon: M1/M2/M3/M4） |
| `Darwin x86_64` | macOS（Intel） |
| `Linux x86_64` | Linux（x86_64） |
| `Linux aarch64` | Linux（arm64） |

#### ステップ 2: ダウンロードして配置

自分の環境に合ったコマンドを **1つだけ** コピーして実行してください：

**macOS（Apple Silicon: M1/M2/M3/M4）の場合：**

```bash
curl -L https://github.com/BambooTuna/tmuxui/releases/latest/download/tmuxui_1.0.0_darwin_arm64.tar.gz -o tmuxui.tar.gz
```

**macOS（Intel）の場合：**

```bash
curl -L https://github.com/BambooTuna/tmuxui/releases/latest/download/tmuxui_1.0.0_darwin_amd64.tar.gz -o tmuxui.tar.gz
```

**Linux（x86_64）の場合：**

```bash
curl -L https://github.com/BambooTuna/tmuxui/releases/latest/download/tmuxui_1.0.0_linux_amd64.tar.gz -o tmuxui.tar.gz
```

**Linux（arm64）の場合：**

```bash
curl -L https://github.com/BambooTuna/tmuxui/releases/latest/download/tmuxui_1.0.0_linux_arm64.tar.gz -o tmuxui.tar.gz
```

#### ステップ 3: 展開してインストール

ダウンロードしたファイルを展開し、コマンドとして使えるように配置します：

```bash
# ダウンロードしたファイルを展開
tar xzf tmuxui.tar.gz

# コマンドとして使えるように配置（パスワードを聞かれたら Mac のログインパスワードを入力）
sudo mv tmuxui /usr/local/bin/

# ダウンロードした圧縮ファイルを削除（不要になったため）
rm tmuxui.tar.gz
```

#### ステップ 4: 動作確認

```bash
tmuxui --help
```

`Usage of tmuxui:` と表示されれば成功です。

> 最新バージョンは [GitHub Releases](https://github.com/BambooTuna/tmuxui/releases) で確認できます。上記 URL のバージョン番号部分を置き換えてください。

### 方法 3: ソースからビルド

Go がインストール済みで、最新の開発版を使いたい場合向けです。

```bash
# ソースコードをダウンロード
git clone https://github.com/BambooTuna/tmuxui.git
cd tmuxui

# ビルド（カレントディレクトリに tmuxui ファイルが生成されます）
go build -o tmuxui .

# 動作確認
./tmuxui --help
```

どこからでも `tmuxui` コマンドで使いたい場合は、PATH の通った場所に配置します：

```bash
sudo mv tmuxui /usr/local/bin/
```

---

## 📲 スマートフォンからアクセスする

Tailscale Serve を使い、HTTPS 経由で安全にアクセスします。SSH クライアントやポートフォワードは不要です。

### 事前準備

1. **MacBook**: `brew install --cask tailscale` でインストール
2. **iPhone**: App Store から [Tailscale](https://apps.apple.com/app/tailscale/id1470499037) をインストール
3. 両方のデバイスで同じアカウントでログイン

> 無料プランで十分です（個人利用: 3ユーザー・100デバイスまで）

### 起動（コピペで完了）

```bash
# tmuxui 起動 + Tailscale Serve で HTTPS 公開
tmuxui & tailscale serve --bg 6062

# アクセス URL を表示
echo "https://$(tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')"
```

表示された URL を iPhone の Safari で開けばアクセスできます。

> **⚠️ 「Serve is not enabled on your tailnet」と表示された場合**: Tailscale Serve が tailnet で有効になっていません。エラーメッセージに表示される URL（`https://login.tailscale.com/f/serve?node=...`）をブラウザで開いて、Serve 機能を有効にしてから再度コマンドを実行してください。

> Tailscale Serve は Tailnet 内の認証済みデバイスのみアクセス可能なため、token なしでも安全です。共有 Tailnet で他ユーザーからのアクセスも制限したい場合は `tmuxui --token mytoken` で起動してください。

> **Tips**: Safari で URL をホーム画面に追加すると、次回からワンタップでアクセスできます。

### 停止

```bash
tailscale serve --bg off
# tmuxui は Ctrl+C で停止
```

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

`~/.config/tmuxui/snippets/` に JSON ファイルを配置することで、Web UI からスニペットを呼び出せます。

```
~/.config/tmuxui/snippets/
├── build.json
├── test.json
└── deploy.json
```

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
| `--token` | (自動生成) | 認証トークン（`TMUXUI_TOKEN` 環境変数でも設定可） |
| `--dev` | false | 開発モード（HTML/CSS/JS をファイルシステムから直接読み込み） |

## 🔒 セキュリティ

- **認証**: 全リクエストに `?token=xxx` が必須（起動時にランダム生成 or 固定指定）
- **localhost バインド**: デフォルトは `127.0.0.1` のみ（外部からアクセス不可）
- **外部アクセス**: Tailscale Serve 経由で HTTPS + Tailnet 認証

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
