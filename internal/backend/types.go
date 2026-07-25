// Package backend defines the PaneBackend abstraction shared by all terminal/session
// backends (tmux control mode, herdr, ...), the common Session/Window/Pane data model,
// the BackendRegistry that resolves prefixed target identifiers to a concrete backend,
// and small helpers (Broadcaster, RecoverAndLog) reused by backend implementations.
package backend

import "errors"

// ErrUnsupported はバックエンドがサポートしない操作を呼び出したときに返す。
// 例えば読み取り専用のバックエンドがSendKeysやNewSessionを拒否する場合など。
var ErrUnsupported = errors.New("backend: operation not supported")

// SnapshotHistoryLines はcapture-pane相当の処理で遡る行数。tmuxui発セッションではデフォルトの
// history-limit(2000)のままだとこの行数まで遡れないため、tmuxctl.New()相当の処理で同じ値まで
// 引き上げる。tmux/herdr両バックエンドが同じ値を共有する。
const SnapshotHistoryLines = 20000

// PaneBackend はセッション/ウィンドウ/ペインの一覧・入出力・CRUDを抽象化する。
// target/nameは各バックエンドのネイティブ形式(プレフィックスなし)を渡すこと。
// プレフィックス解決はBackendRegistryの責務。
type PaneBackend interface {
	// ListSessions は管理下の全セッションをネイティブなtarget形式で返す。
	ListSessions() ([]Session, error)
	// SyncSessions はListSessions()で取得した最新の一覧を使い、内部状態(ControlSession等)を
	// 同期する。同期処理が不要なバックエンドはno-opでよい。
	SyncSessions(sessions []Session)

	Snapshot(target string) (data []byte, cols int, rows int, err error)
	Subscribe(target string) (<-chan []byte, func(), error) // stream, cancel
	// CapturePane はポーリング表示・permission検知向けの平文+ANSI付きキャプチャを返す。
	CapturePane(target string) (*PaneContent, error)
	// CapturePanePlain はpermission検知用の可視画面のみの軽量キャプチャを返す。
	CapturePanePlain(target string) (string, error)

	SendKeys(target, keys string) error
	Resize(target string, cols, rows int) error

	NewSession(name, dir string) error
	KillSession(name string) error
	RenameSession(oldName, newName string) error

	NewWindow(sessionName, windowName string) error
	KillWindow(target string) error
	RenameWindow(target, newName string) error

	KillPane(target string) error
	SplitPane(target string, horizontal bool) error

	// OnTopologyChange はセッション/ウィンドウ/ペイン構成が変化した際に呼ばれるコールバックを登録する。
	OnTopologyChange(fn func())

	// ValidTarget はsがこのバックエンドにとって構文的に正しいtarget/name(ネイティブ形式)かを返す。
	ValidTarget(s string) bool

	// SupportsTextPermissionDetection はHub側のdetectPermission(画面テキストのヒューリスティック解析)を
	// このバックエンドのペインに対して実行すべきかを返す。herdrのようにagent_statusという構造化された
	// 状態を持つバックエンドではfalseを返し、二重の権限待ち通知を防ぐ。
	SupportsTextPermissionDetection() bool
}

type Pane struct {
	Target string `json:"target"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Cmd    string `json:"cmd"`
	Size   string `json:"size"`
	Path   string `json:"path"`
	// Agent/AgentStatus はherdrバックエンドのみが設定する(agent種別/idle・working・blocked・done・unknown)。
	// tmuxバックエンドでは常に空文字列のままで、後方互換性に影響しない。
	Agent       string `json:"agent,omitempty"`
	AgentStatus string `json:"agent_status,omitempty"`
}

type Window struct {
	Index  int    `json:"index"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	// AgentStatus はherdrのtab.list由来のウィンドウ単位の集約状態(herdrバックエンドのみ設定)。
	AgentStatus string `json:"agent_status,omitempty"`
	Panes       []Pane `json:"panes"`
}

type Session struct {
	Name     string `json:"name"`
	Backend  string `json:"backend"`
	Attached bool   `json:"attached"`
	// DisplayName はUI表示用のラベル(herdrのworkspace.label)。空ならフロントエンドはNameにフォールバックする。
	DisplayName string `json:"display_name,omitempty"`
	// AgentStatus はherdrのworkspace.list由来のセッション単位の集約状態(herdrバックエンドのみ設定)。
	AgentStatus string `json:"agent_status,omitempty"`
	// WorktreeLabel はherdrのworktree情報から組み立てた表示用ラベル(例: "repo · branch")。
	WorktreeLabel string   `json:"worktree_label,omitempty"`
	Windows       []Window `json:"windows"`
}

type PaneContent struct {
	Target  string `json:"target"`
	Content string `json:"content"`
	Lines   int    `json:"lines"`
	Ts      int64  `json:"ts"`
}
