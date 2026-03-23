# tmuxui

[![Release](https://img.shields.io/github/release/BambooTuna/tmuxui.svg)](https://github.com/BambooTuna/tmuxui/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

スマートフォンから tmux セッションを監視・操作できる Web アプリ。Claude Code エージェントチームの権限許可・コマンド送信を外出先から実行可能。

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

- tmux セッション・ペイン一覧と内容のリアルタイム閲覧
- Claude Code の権限許可を承認・拒否（AUTO モードあり）
- ペインへのキー入力・コマンド送信
- セッション管理（作成・削除・名前変更）
- スニペット（`~/.config/tmuxui/snippets/*.json`）

## CLI オプション

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--port` | 6062 | リッスンポート |
| `--host` | 127.0.0.1 | バインドアドレス |
| `--token` | (自動生成) | 認証トークン（`TMUXUI_TOKEN` 環境変数でも可） |
| `--dev` | false | 開発モード |

## ライセンス

MIT - [LICENSE](./LICENSE)
