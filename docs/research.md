# BambooTuna tmuxui - 既存ソリューション調査レポート

**調査日**: 2026年2月21日
**調査対象**: tmux Web UI、スマホアクセス、Claude Code統合ツール

---

## 1. tmux Web UI系ツール

### 1.1 GoTTY
[GitHub - yudai/gotty](https://github.com/yudai/gotty)

| 項目 | 内容 |
|------|------|
| **概要** | CLI ツールをWebアプリケーションに変換するツール。ブラウザからターミナルを表示・操作可能 |
| **言語** | Go言語 |
| **フロントエンド** | xterm.js、hterm |
| **通信** | WebSocket |
| **デフォルトポート** | 8080 |
| **tmux統合** | 可能（`gotty tmux new -A -s gotty top` など） |

**長所**:
- シンプルで軽量な実装
- セキュリティオプションが豊富（基本認証、TLS/SSL、クライアント証明書）
- マルチプラットフォーム対応（macOS、Linux、FreeBSD、Windows）
- デフォルトでクライアント入力制限

**短所**:
- tmuxペイン個別閲覧の複雑さ
- 権限許可操作の専用機能なし
- UI カスタマイズ性が低い
- モバイル対応が基本的

**推奨度**: ⭐⭐⭐（基本的だが、要件を満たすには改造が必要）

---

### 1.2 ttyd
[GitHub - tsl0922/ttyd](https://github.com/tsl0922/ttyd)

| 項目 | 内容 |
|------|------|
| **概要** | ターミナルセッションをWebブラウザ経由で共有するツール |
| **言語** | C 56.1%、TypeScript 27.0% |
| **実装基盤** | libuv、WebGL2 |
| **通信** | WebSocket（Libwebsockets） |
| **セキュリティ** | OpenSSL/Mbed TLS ベース |

**機能**:
- 日中韓（CJK）・IME 完全対応
- ZMODEM ファイル転送対応
- Sixel 画像出力対応
- ファイル転送（trzsz）
- カスタムコマンド実行

**長所**:
- 高速でパフォーマンスに優れている
- 国際化対応が充実
- 機能が豊富（ファイル転送、画像出力）

**短所**:
- tmuxペイン管理の仕様が不明
- 権限許可操作の組み込み機能がない
- ドキュメントに詳細な説明が不足

**推奨度**: ⭐⭐⭐（パフォーマンスは優秀だがtmux特化度が低い）

---

### 1.3 WebMux
[GitHub - nooesc/webmux](https://github.com/nooesc/webmux)

| 項目 | 内容 |
|------|------|
| **概要** | Webベースの TMUX セッションビューア・コントローラー |
| **言語** | Rust（バックエンド）、Vue 3 + TypeScript（フロントエンド） |
| **フレームワーク** | Axum（バックエンド）、Vue 3（フロントエンド） |
| **通信** | WebSocket |
| **スタイリング** | Tailwind CSS |
| **デプロイ形式** | PWA（Progressive Web App） |

**主要機能**:
- tmuxセッションの作成、リネーム、削除
- ウィンドウの作成・切り替え・削除
- リアルタイム端末I/O
- xterm.js ベースのターミナルエミュレーション

**モバイル対応**:
- iOS: Safari 経由で「ホーム画面に追加」
- Android: Chrome 経由でホーム画面追加またはアプリインストール
- タッチフレンドリーインターフェース
- iOS セーフエリア対応

**長所**:
- **モバイルフレンドリーな設計** ⭐
- **モダンな技術スタック**（Rust + Vue 3）
- PWA対応で、モバイルからのインストール可能
- tmuxセッション・ウィンドウ管理が組み込まれている
- パフォーマンスに優れている（Rust）

**短所**:
- プロジェクト成熟度の確認が必要
- 権限許可操作の実装状況が不明
- Claude Code 専用機能がない
- HTTPS 必須

**推奨度**: ⭐⭐⭐⭐⭐（モバイル対応の優秀さと拡張性）

---

### 1.4 WebTMUX
[GitHub - nonoxz/webtmux](https://github.com/nonoxz/webtmux)

| 項目 | 内容 |
|------|------|
| **概要** | tmuxセッションをWebブラウザ経由でインタラクト可能にするプロジェクト |
| **言語** | JavaScript/Node.js |
| **バックエンド** | Express.js |
| **通信** | Socket.io |
| **フロントエンド** | xterm.js |

**長所**:
- シンプルなNode.js スタック
- Socket.io によるリアルタイム双方向通信
- 学習コストが低い

**短所**:
- プロジェクトの成熟度が低い可能性
- モバイル最適化の状況が不明
- 権限許可操作が未実装
- パフォーマンスの懸念（Node.js）

**推奨度**: ⭐⭐（基本実装はあるが、要件充足に不安あり）

---

### 1.5 Wetty
[GitHub - wetty](https://github.com/butlerx/wetty)

| 項目 | 内容 |
|------|------|
| **概要** | Node.js ベースのWebターミナル。SSH/ログイン対応 |
| **実装** | hterm + WebSocket ベース |
| **用途** | SSH経由でのリモートターミナルアクセス |

**長所**:
- SSH統合が標準

**短所**:
- tmux 特化度が低い
- ペイン管理機能がない
- モバイル対応が限定的

**推奨度**: ⭐⭐（tmux特化度が不足）

---

## 2. Claude Code 専用 tmux 管理ツール

### 2.1 Claude Code Agent Teams（組み込み機能）
[Claude Code Docs - Agent Teams](https://code.claude.com/docs/en/agent-teams)
[GitHub - disler/claude-code-hooks-multi-agent-observability](https://github.com/disler/claude-code-hooks-multi-agent-observability)

| 項目 | 内容 |
|------|------|
| **形態** | Claude Code の組み込み機能 |
| **基本概念** | チームリードがタスクを割り当て、複数エージェントが並行実行 |
| **通信方式** | TaskUpdate、SendMessage ツール |
| **テクノロジー** | tmux（split-pane）または iTerm2（tabs/windows） |

**権限管理**:
- すべてのチームメンバーはリードの権限モードを継承
- 個別メンバーの権限モード変更は可能
- 事前に操作をホワイトリスト化して許可可能

**モニタリング機能**:
- キーボードショートカット（Shift+Up/Down で切り替え）
- タスクリスト表示（Ctrl+T）
- セッション内容表示（Enter）
- リアルタイムobservability ダッシュボード対応

**長所**:
- **Claude Code ネイティブ統合**
- **複数エージェントの権限管理機能**
- タスク・メッセージングシステム完備
- tmux split-pane 対応

**短所**:
- **スマートフォンアクセスが想定されていない** ❌
- リード側のセッションが必須
- PCベースのUI（Webブラウザ想定なし）

**推奨度**: ⭐⭐⭐（権限管理面では優秀だが、モバイルアクセスが課題）

---

## 3. ターミナル共有 OSS プロジェクト

### 3.1 Webpair
[GitHub - yarmand/webpair](https://github.com/yarmand/webpair)

| 項目 | 内容 |
|------|------|
| **概要** | ローカルの tmux セッションをリモートブリッジ経由でWebブラウザに共有 |
| **用途** | ペアプログラミング |
| **通信** | WebSocket（リモートブリッジ経由） |

**長所**:
- tmux統合が深い
- ペアプログラミング向け

**短所**:
- セットアップが複雑（リモートブリッジ必須）
- モバイル対応の明言がない
- 権限許可機能が不明

**推奨度**: ⭐⭐（セットアップ複雑性が高い）

---

### 3.2 tmate
[tmate - Instant terminal sharing](https://tmate.io/)

| 項目 | 内容 |
|------|------|
| **概要** | tmux の派生。ターミナル共有用フォーク |
| **特徴** | 即座にセッション共有用URL生成 |
| **インストール** | Homebrew など標準パッケージマネージャ対応 |

**長所**:
- セットアップが簡単
- 即座に共有URL生成

**短所**:
- 権限管理機能が限定的
- モバイル表示最適化がない
- 指示送信機能がない

**推奨度**: ⭐⭐（共有機能は優秀だが、要件充足度が低い）

---

## 4. スマートフォン向け tmux リモートアクセスソリューション

### 4.1 Reattach（iOS）
[Reattach - tmux remote App（App Store）](https://apps.apple.com/us/app/reattach-tmux-remote/id6757171671)

| 項目 | 内容 |
|------|------|
| **プラットフォーム** | iOS（iOS 18.0以上） |
| **言語** | Swift（推定） |
| **サーバーサイド** | reattachd（オープンソース） |
| **リモートアクセス** | VPN または Cloudflare Tunnel |

**機能**:
- tmuxセッション・ペインの閲覧と制御
- キーボード入力サポート
- 複数セッション・ペイン間の切り替え
- ワンタップ実行コマンド
- プッシュ通知対応

**長所**:
- **iPhoneネイティブアプリ** ⭐
- **セッション・ペイン制御対応** ⭐
- リモートセッション共有対応（VPN/Tunnel）
- Claude Code スタック内での使用例あり

**短所**:
- **iOS のみ対応**（Android未対応）
- サーバー側に reattachd デーモン必須
- セットアップに中程度の複雑さ
- サーバーサイド実装の詳細が不明

**推奨度**: ⭐⭐⭐⭐（iOS ユーザーには最良だがAndroid未対応）

---

### 4.2 Muxile（tmux プラグイン）
[GitHub - bjesus/muxile](https://github.com/bjesus/muxile)

| 項目 | 内容 |
|------|------|
| **形態** | tmux プラグイン |
| **実装** | WebSocket（Cloudflare Worker経由） |
| **アクセス** | QRコード + ブラウザ |

**仕組み**:
- Cloudflare Worker が WebSocket サーバーとして動作
- websocat で Unix socket 経由に tmux とデータ通信
- QRコード生成でブラウザアクセス

**長所**:
- **専用アプリ不要** ⭐
- **Cloudflare無料プランで動作可能** ⭐
- セットアップが比較的簡単

**短所**:
- **外部サービス（Cloudflare）への依存** ⚠️
- セッション共有の際に Cloudflare 経由
- Cloudflare のレート制限・可用性に依存
- ペイン個別制御の詳細が不明

**推奨度**: ⭐⭐⭐（外部依存が許容なら有効）

---

### 4.3 Tailscale + Termius/Termux
[Tailscale](https://tailscale.com/) + [Termius（iOS/Android）](https://termius.com/) または Termux

| 項目 | 内容 |
|------|------|
| **構成** | Tailscale（VPN）+ SSH + tmux |
| **対応** | iOS、Android 両対応 |
| **SSH クライアント** | Termius（商用・無料）または Termux（Android） |

**セットアップ**:
1. Tailscale を Mac・iPhone/Android に導入
2. リモートログイン有効化（macOS）
3. Termius/Termux で SSH 接続
4. tmux attach

**長所**:
- **iOS・Android 両対応** ⭐⭐
- **NAT/ファイアウォール透過** ⭐
- セットアップが直感的
- セッション永続性を tmux で確保

**短所**:
- **複数ツール組み合わせ必須**（複雑化）
- Termius 有料機能が必要な場合あり
- tmux セッション操作の手動性（ペイン一覧など）
- Tailscale への月額コスト（商用利用）

**推奨度**: ⭐⭐⭐⭐（汎用性は高いがセットアップ複雑）

---

## 5. Claude Code エージェントチーム向けモニタリングツール

### 5.1 Claude Code Agent Teams - 組み込みモニタリング
[Claude Code Docs - Agent Teams Controls](https://code.claude.com/docs/en/agent-teams)

| 項目 | 内容 |
|------|------|
| **形態** | Claude Code 組み込み機能 |
| **モニタリング方式** | キーボードショートカット + tmux split-pane |
| **実装** | タスクシステム + メッセージング |

**UI制御**:
- Shift+↑/↓: チームメンバー切り替え
- Ctrl+T: タスク一覧表示
- Enter: 当該セッション表示
- Esc: 割り込み

**長所**:
- **Claude Code ネイティブ** ⭐
- **リアルタイムモニタリング** ⭐
- タスク追跡が組み込まれている

**短所**:
- PC の CLI ベース（スマートフォン未対応）
- tmux/iTerm2 が必須
- Webブラウザインターフェースがない

**推奨度**: ⭐⭐⭐⭐（PC からの制御・監視には最適）

---

### 5.2 Datadog AI Agents Console
[Datadog Blog - Claude Code Monitoring](https://www.datadoghq.com/blog/claude-code-monitoring/)
[SigNoz Dashboard Template](https://signoz.io/docs/dashboards/dashboard-templates/claude-code-dashboard/)

| 項目 | 内容 |
|------|------|
| **形態** | 外部ダッシュボード（SaaS） |
| **対応** | Datadog、SigNoz など |
| **メトリクス** | トークン消費、コスト、成功率、実行時間 |

**機能**:
- エージェント swim lane ビュー
- タスク生命周期追跡
- 障害検知
- 組織レベルの採用・パフォーマンス分析

**長所**:
- **Webダッシュボード対応** ⭐
- **スマートフォンで監視可能**（Webブラウザ）
- メトリクス・コスト分析に優れている

**短所**:
- **外部SaaS（有料）** ⚠️
- Claude Code の hook イベント通知設定が必要
- リアルタイム制御・指示送信機能がない
- セットアップに DevOps知識必要

**推奨度**: ⭐⭐⭐（エージェント監視・分析重視なら有効。ただし有料）

---

### 5.3 Claude Code Hooks Multi-Agent Observability
[GitHub - disler/claude-code-hooks-multi-agent-observability](https://github.com/disler/claude-code-hooks-multi-agent-observability)

| 項目 | 内容 |
|------|------|
| **形態** | オープンソースツール（hook ベース） |
| **実装** | Claude Code hooks イベント追跡 |
| **通信** | WebSocket など |

**長所**:
- hook ベースでシンプル
- Datadog などと統合可能

**短所**:
- セットアップが必要
- リアルタイム制御機能がない
- 権限許可操作への対応状況が不明

**推奨度**: ⭐⭐⭐（既存 observability 基盤への統合向け）

---

## 6. 比較表 - 要件適合度評価

| ツール | tmuxペイン閲覧 | 権限許可操作 | 指示送信 | 制限操作 | モバイル対応 | セットアップ難易度 |
|--------|:---:|:---:|:---:|:---:|:---:|:---:|
| **GoTTY** | ⭐⭐ | ❌ | ⭐ | ⭐⭐⭐ | ⭐⭐ | 簡単 |
| **ttyd** | ⭐⭐ | ❌ | ⭐ | ⭐⭐⭐ | ⭐⭐ | 簡単 |
| **WebMux** | ⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ | 中程度 |
| **WebTMUX** | ⭐⭐⭐ | ⭐ | ⭐ | ⭐⭐ | ⭐⭐ | 中程度 |
| **Wetty** | ⭐ | ❌ | ⭐ | ⭐⭐ | ⭐ | 簡単 |
| **Claude Code Agent Teams** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ❌❌ | 中程度 |
| **Webpair** | ⭐⭐⭐ | ⭐ | ⭐ | ⭐⭐ | ⭐ | 複雑 |
| **tmate** | ⭐⭐⭐ | ⭐ | ⭐ | ⭐⭐ | ⭐⭐ | 簡単 |
| **Reattach（iOS）** | ⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐（iOS） | 中程度 |
| **Muxile** | ⭐⭐⭐ | ⭐ | ⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ | 簡単 |
| **Tailscale+Termius** | ⭐⭐⭐ | ⭐ | ⭐⭐ | ⭐⭐ | ⭐⭐⭐⭐ | 中程度 |
| **Datadog Console** | ⭐ | ❌ | ❌ | ⭐⭐ | ⭐⭐⭐（Web） | 複雑 |

---

## 7. 推奨アーキテクチャ

### 7.1 推奨アプローチ：ハイブリッド構成

**BambooTuna の要件を最適に満たすには、以下の組み合わせを推奨します**:

```
PC 側（デスク）:
├─ Claude Code (リード) + Agent Teams → tmux split-pane モニタリング
└─ Agent Workers → tmux セッション実行

スマートフォン側:
├─ WebMux（または カスタム Webアプリ）
│  ├─ tmux ペイン一覧表示
│  ├─ 権限許可ボタン UI
│  └─ Claude Code 指示入力フォーム
└─ Reattach（iOS）or Muxile（汎用）
   └─ 緊急時・軽操作用
```

### 7.2 実装アプローチ（段階別）

#### **フェーズ 1: MVP（スマートフォンからの閲覧・承認）**

**基盤技術**:
- **バックエンド**: WebMux または ttyd（Go/Rust）
- **フロントエンド**: PWA（Vue 3 + Tailwind CSS）
- **通信**: WebSocket
- **認証**: Claude Code hooks との統合

**機能**:
1. tmux ペイン一覧表示（リードセッション取得）
2. 権限許可ボタン UI（hook経由で Claude Code に回答）
3. 基本的なセッション情報表示

#### **フェーズ 2: 指示送信機能**

**追加機能**:
1. SendMessage フォーム（テキスト入力）
2. 定型テンプレートコマンド
3. タスク一覧表示・割り当て

#### **フェーズ 3: 高度な操作**

**追加機能**:
1. ターミナル出力のリアルタイム閲覧
2. 簡易エディタ（コード差分確認）
3. タスク完了マーク機能

---

## 8. 最終推奨事項

### 8.1 短期推奨（実装 3-4 週間）

**採用技術**:
1. **基盤**: WebMux をベースに拡張
   - ✅ Rust 実装で堅牢
   - ✅ PWA 対応でモバイル最適化済み
   - ✅ Vue 3 で UIカスタマイズ容易
   - ✅ tmux 統合機能あり

2. **統合方式**: Claude Code hooks
   - リード側で permission request を hook で検知
   - スマートフォン側で承認 UI を表示
   - SendMessage 経由で指示入力

3. **モバイル**: iOS + Android 両対応（PWA）

### 8.2 長期推奨（実装 2-3 ヶ月）

1. **Reattach との連携**（iOS ネイティブ強化）
2. **Muxile への貢献**（専用ペイン制御 UI）
3. **Claude Code 公式プラグイン化**

### 8.3 避けるべきアプローチ

❌ **単一ツールへの依存**
- Datadog など SaaS オンリーではコスト化
- Muxile 単独では権限管理に不足

❌ **GoTTY・ttyd ベース**
- tmux ペイン管理が弱い
- カスタマイズ性が低い

❌ **Tailscale + Termius の組み合わせのみ**
- セットアップ複雑、UI/UX が不便
- モバイルアクセスが手動的

---

## 9. 参考資料リスト

### Web UI 系ツール
- [GitHub - yudai/gotty](https://github.com/yudai/gotty)
- [GitHub - tsl0922/ttyd](https://github.com/tsl0922/ttyd)
- [GitHub - nonoxz/webtmux](https://github.com/nonoxz/webtmux)
- [GitHub - nooesc/webmux](https://github.com/nooesc/webmux)

### Claude Code Agent Teams
- [Claude Code Docs - Agent Teams](https://code.claude.com/docs/en/agent-teams)
- [GitHub - disler/claude-code-hooks-multi-agent-observability](https://github.com/disler/claude-code-hooks-multi-agent-observability)

### モバイルアクセスソリューション
- [Reattach - iOS App](https://apps.apple.com/us/app/reattach-tmux-remote/id6757171671)
- [GitHub - bjesus/muxile](https://github.com/bjesus/muxile)
- [Tailscale](https://tailscale.com/)
- [Termius](https://termius.com/)

### 参考ブログ・記事
- [Elliot Bonneville - Seamless Claude Code Handoff](https://elliotbonneville.com/phone-to-mac-persistent-terminal/)
- [Sameer Halai - Access Claude Code from phone with tmux & Tailscale](https://sameerhalai.com/blog/access-your-desktop-claude-code-session-from-your-phone-using-tmux-tailscale/)
- [Datadog - Claude Code Monitoring](https://www.datadoghq.com/blog/claude-code-monitoring/)

---

**報告日**: 2026年2月21日
**調査者**: researcher（Claude Haiku 4.5）
