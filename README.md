# tmuxui

[![Release](https://img.shields.io/github/release/BambooTuna/tmuxui.svg)](https://github.com/BambooTuna/tmuxui/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

スマホから **tmux / [herdr](https://github.com/BambooTuna/herdr)** のセッションを監視・操作できる Web アプリ。Claude Code などのエージェントに投げた作業の状況を眺めつつ、権限承認・追加指示・ファイル授受を外出先からこなせる。

|  |  |
|:--:|:--:|
| **セッション一覧** (herdr / tmux 切替、エージェント状態ドット) | **設定** (自動アップデート・バージョン切替) |
| <img src="docs/screenshots/list.png" width="320"> | <img src="docs/screenshots/settings.png" width="320"> |
| **ペイン詳細: システム監視** (xterm.js で btop をそのまま描画) | **ペイン詳細: エージェント対話** (Claude Code に外出先から声かけ) |
| <img src="docs/screenshots/pane_detail.png" width="320"> | <img src="docs/screenshots/pane_chat.png" width="320"> |
| **拡張キーシート** (Ctrl / PgUp/Dn / ←→ / Backspace などを片手で) | **ファイラー** (参照・アップロード・HTML プレビュー) |
| <img src="docs/screenshots/pane_keysheet.png" width="320"> | <img src="docs/screenshots/filer.png" width="320"> |

## なぜ

AI エージェントに任せる開発が当たり前になり、「投げてから戻ってくるまで」の待ち時間がどんどん長くなった。一方で PC の前に張り付いている必然性は薄れ、その時間に別のプロジェクトを進めたり、外に出たりしたい場面が増えた。

tmuxui は「PC を離れている間もエージェントを止めない」ためのスマホ側リモコン。電車の中や旅先から複数プロジェクトの状況をチェックして、必要なら追加指示・権限承認・ファイル差し込みが片手でできる。

## インストール

### バイナリダウンロード（推奨）

```bash
curl -sL https://api.github.com/repos/BambooTuna/tmuxui/releases/latest \
  | grep "browser_download_url.*$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')\.tar\.gz" \
  | cut -d '"' -f 4 \
  | xargs curl -sL | tar xz -C /usr/local/bin tmuxui
```

### go install

```bash
go install github.com/BambooTuna/tmuxui@latest
```

### 動作確認

```bash
tmuxui --help
```

### Docker（デーモン化・自動起動向け）

ホストの tmux / herdr socket をコンテナにマウントする方式。サーバ本体はホストで動かし、コンテナ内の tmuxui はそのクライアントとして接続するだけ。systemd を直接触らずに `restart: unless-stopped` で PC 再起動後も自動起動・自動復旧させたい場合に使う。

前提:

- Linux ホスト（`network_mode: host` と socket マウントに依存するため、macOS Docker Desktop は非サポート。macOS はネイティブ実行を推奨）
- ホストで tmux または herdr が起動していること（サーバはホスト側、tmuxui はコンテナ側というワンウェイ構成）

`.env`（`docker-compose.example.yml` と同じディレクトリに配置）:

```bash
UID=1000
GID=1000
TMUXUI_TOKEN=your-secret-token
```

`docker-compose.example.yml` を `docker-compose.yml` としてコピーし、そのまま起動する:

```bash
cp docker-compose.example.yml docker-compose.yml
docker compose up -d
# http://127.0.0.1:6062?token=your-secret-token
```

image は `ghcr.io/bambootuna/tmuxui:latest`（GitHub Actions のリリースフローで amd64/arm64 multi-arch push 済み）。

備考:

- 自動アップデートは有効（設定画面からその場で適用できる）。ただし適用してもコンテナのファイルシステムを書き換えているだけなので、次回 `docker compose up -d` で image のバージョンに戻る。恒久更新は `docker compose pull && docker compose up -d`。オフにしたい場合は環境変数 `TMUXUI_AUTOUPDATE=0` を渡す。
- filer で見たいホストパスは `docker-compose.yml` の `volumes:` に明示的に追記しないと見えない（デフォルトでは compose がマウントしたパス以外はコンテナから不可視）。

## 使い方

### ローカル起動

```bash
tmuxui
# http://127.0.0.1:6062?token=xxx（起動時に表示される URL をブラウザで開く）
```

### スマートフォンからアクセス

#### すぐ試す: Cloudflare Quick Tunnel（無料・アカウント不要）

[cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) をインストールするだけ。

```bash
cloudflared tunnel --url http://localhost:6062 &
tmuxui --token mytoken  # Ctrl+C で停止
```

表示された `*.trycloudflare.com` URL にスマホからアクセス。公開 URL のため `--token` 必須。URL は毎回変わる。

#### 常用する: Tailscale Serve（無料・要アカウント）

PC・スマホ両方に [Tailscale](https://tailscale.com/) をインストールし、同じアカウントでログイン（無料枠: 3ユーザー・100デバイス）。Tailnet 内の認証済みデバイスのみアクセスでき、固定 URL で HTTPS も自動。

```bash
tailscale serve --bg 6062
echo "https://$(tailscale status --json | jq -r '.Self.DNSName' | sed 's/\.$//')"
tmuxui  # Ctrl+C で停止
```

```bash
# Tailscale Serve の停止
tailscale serve --bg off
```

## 主な機能

- **tmux / herdr 両対応** — 起動時に自動判別、上部タブで切替
- **リアルタイム閲覧** — セッション・ウィンドウ・ペインを一覧化、Claude Code のエージェント状態（作業中 / 待機）をドット表示
- **xterm.js ベースの描画** — ANSI カラー・スクロールバック対応。バッファをプレーンテキストにしてコピー可能
- **キー入力・コマンド送信** — 矢印 / Enter / Ctrl + 任意キー / スニペットをスマホから片手で
- **スニペット** — `~/.config/tmuxui/snippets/*.json` に定型プロンプトを置いてワンタップ送信
- **権限承認 (AUTO モードあり)** — Claude Code の権限プロンプトをスマホから即応
- **ファイラー** — ホスト側ディレクトリの参照、`~/.tmuxui/uploads/YYYY-MM-DD/` へのアップロード、HTML ファイルのブラウザプレビュー
- **セッション作成・削除・改名** — スマホ操作で新しい作業単位を立ち上げ
- **自動アップデート・バージョン切替** — 設定画面から任意のリリースバージョンにその場で切替
- **モバイル最適化 UI** — タッチスワイプでスクロール、片手キーシート、Apple Intelligence 風の視覚言語

## CLI オプション

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--port` | 6062 | リッスンポート |
| `--host` | 127.0.0.1 | バインドアドレス |
| `--token` | (自動生成) | 認証トークン（`TMUXUI_TOKEN` 環境変数でも可） |
| `--dev` | false | 開発モード |

## ライセンス

MIT - [LICENSE](./LICENSE)
