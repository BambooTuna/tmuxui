package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

// selectableMinVersion より小さいタグはバージョン選択 UI に載せない。
// v2.0.0 で「任意バージョン切替」機能が入ったので、それ以前を選ばせても機能が動かない。
const selectableMinVersion = "v2.0.0"

// globalUpdateManager はサーバー起動時に main.go から組み立てられる (globalPreferences と同じ流儀)
var globalUpdateManager *UpdateManager

// UpdateStatus はアップデートの現在状態。HTTP/WebSocket 双方でそのまま JSON 化して使う。
type UpdateStatus struct {
	Current       string    `json:"current"`
	Latest        string    `json:"latest,omitempty"`
	HasUpdate     bool      `json:"hasUpdate"`
	LastCheckedAt time.Time `json:"lastCheckedAt,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
	Applied       bool      `json:"applied,omitempty"` // 差し替え済み(再起動待ち)
	AppliedAt     time.Time `json:"appliedAt,omitempty"`
}

// newSelfUpdater は go-selfupdate の Updater をチェックサム検証付きで生成する。
func newSelfUpdater() (*selfupdate.Updater, error) {
	return selfupdate.NewUpdater(selfupdate.Config{
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
	})
}

// checkForUpdate は最新リリースを取得するだけで適用は行わない。
// version == "dev" は開発ビルドとして常に「更新なし」扱い(既存挙動維持)。
// 非semverなバージョン(ローカル手動ビルド等)でLessOrEqualが panic するのを防ぐため recover する。
func checkForUpdate(ctx context.Context) (latest *selfupdate.Release, hasUpdate bool, err error) {
	updater, uerr := newSelfUpdater()
	if uerr != nil {
		return nil, false, uerr
	}
	var found bool
	latest, found, err = updater.DetectLatest(ctx, selfupdate.ParseSlug("BambooTuna/tmuxui"))
	if err != nil {
		return nil, false, fmt.Errorf("update check failed: %w", err)
	}
	if !found {
		return nil, false, fmt.Errorf("no release found for this platform")
	}
	if version == "dev" {
		return latest, false, nil
	}
	defer func() {
		if r := recover(); r != nil {
			hasUpdate = false
		}
	}()
	hasUpdate = !latest.LessOrEqual(version)
	return latest, hasUpdate, nil
}

// detectVersion は指定タグ (先頭 v は許容) の Release を取得する。
// go-selfupdate の DetectVersion は release.TagName との完全一致 (=先頭v付き) を要求するため、
// v を落とさず、無ければ付けて渡す。
func detectVersion(ctx context.Context, versionStr string) (*selfupdate.Release, error) {
	updater, err := newSelfUpdater()
	if err != nil {
		return nil, err
	}
	v := versionStr
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	rel, found, err := updater.DetectVersion(ctx, selfupdate.ParseSlug("BambooTuna/tmuxui"), v)
	if err != nil {
		return nil, fmt.Errorf("detect version %s failed: %w", versionStr, err)
	}
	if !found {
		return nil, fmt.Errorf("release %s not found for this platform", versionStr)
	}
	return rel, nil
}

// AvailableRelease は UI 側の select に表示する 1 件。
type AvailableRelease struct {
	Version     string    `json:"version"`
	Name        string    `json:"name,omitempty"`
	PublishedAt time.Time `json:"publishedAt,omitempty"`
	Prerelease  bool      `json:"prerelease,omitempty"`
}

// listSelectableReleases は GitHub REST API から releases を取得し、selectableMinVersion 以上のみ返す。
// go-selfupdate に list API がないため直叩き。認証なし (public repo) で rate limit 60/h。
func listSelectableReleases(ctx context.Context) ([]AvailableRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/BambooTuna/tmuxui/releases?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list releases failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list releases: unexpected status %d", resp.StatusCode)
	}
	var raw []struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	out := make([]AvailableRelease, 0, len(raw))
	for _, r := range raw {
		if r.Draft {
			continue
		}
		if compareSemver(r.TagName, selectableMinVersion) < 0 {
			continue
		}
		out = append(out, AvailableRelease{
			Version:     r.TagName,
			Name:        r.Name,
			PublishedAt: r.PublishedAt,
			Prerelease:  r.Prerelease,
		})
	}
	return out, nil
}

// compareSemver は "vX.Y.Z" 形式を比較する簡易実装。非semver は 0 扱いで無害化する。
func compareSemver(a, b string) int {
	pa := parseSemverTriple(a)
	pb := parseSemverTriple(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseSemverTriple(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	// pre-release 部分 (-rc.1 等) は無視
	if idx := strings.IndexAny(s, "-+"); idx >= 0 {
		s = s[:idx]
	}
	parts := strings.SplitN(s, ".", 3)
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		var n int
		fmt.Sscanf(parts[i], "%d", &n)
		out[i] = n
	}
	return out
}

// applyUpdate は latest のバイナリを取得して自身の実行ファイルと差し替える。
// latest が nil の場合は先にチェックする。
func applyUpdate(ctx context.Context, latest *selfupdate.Release) error {
	updater, err := newSelfUpdater()
	if err != nil {
		return err
	}
	if latest == nil {
		var found bool
		latest, found, err = updater.DetectLatest(ctx, selfupdate.ParseSlug("BambooTuna/tmuxui"))
		if err != nil {
			return fmt.Errorf("update check failed: %w", err)
		}
		if !found {
			return fmt.Errorf("no release found for this platform")
		}
	}
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return err
	}
	if err := updater.UpdateTo(ctx, latest, exe); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	return nil
}

// restartSelf は指定パスの実行ファイルで自プロセスを execve で置換する。
// exe は applyUpdate 前に os.Executable() で取得したパスを渡すこと。apply 後は
// /proc/self/exe が rename された古い inode(.old や" (deleted)")を指すため。
func restartSelf(exe string) error {
	if exe == "" {
		var err error
		exe, err = selfupdate.ExecutablePath()
		if err != nil {
			return err
		}
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// runUpdate は `tmuxui update` CLI サブコマンドの実装。既存挙動(execveせず終了)を維持する薄いラッパー。
func runUpdate() error {
	if os.Getenv("TMUXUI_AUTOUPDATE") == "0" {
		fmt.Println("auto-update disabled by TMUXUI_AUTOUPDATE=0")
		return nil
	}
	ctx := context.Background()
	latest, hasUpdate, err := checkForUpdate(ctx)
	if err != nil {
		return err
	}
	if !hasUpdate {
		fmt.Printf("already up to date (%s)\n", version)
		return nil
	}

	fmt.Printf("updating %s -> %s ...\n", version, latest.Version())
	if err := applyUpdate(ctx, latest); err != nil {
		return err
	}
	fmt.Println("updated successfully")
	return nil
}

// --- UpdateManager: 状態保持と定期チェックループ ---

const (
	defaultIntervalHours = 24
	minIntervalHours     = 1
	maxIntervalHours     = 168
)

// autoUpdatePrefs は preferences.json の "autoUpdate" キーの規約。存在しない/型不一致の場合は既定値を使う。
type autoUpdatePrefs struct {
	Enabled       bool
	AutoApply     bool
	IntervalHours int
}

func loadAutoUpdatePrefs(prefs *Preferences) autoUpdatePrefs {
	out := autoUpdatePrefs{Enabled: true, AutoApply: false, IntervalHours: defaultIntervalHours}
	if prefs == nil {
		return out
	}
	raw, ok := prefs.GetAll()["autoUpdate"].(map[string]any)
	if !ok {
		return out
	}
	if v, ok := raw["enabled"].(bool); ok {
		out.Enabled = v
	}
	if v, ok := raw["autoApply"].(bool); ok {
		out.AutoApply = v
	}
	if v, ok := raw["intervalHours"].(float64); ok {
		out.IntervalHours = int(v)
	}
	if out.IntervalHours < minIntervalHours {
		out.IntervalHours = minIntervalHours
	}
	if out.IntervalHours > maxIntervalHours {
		out.IntervalHours = maxIntervalHours
	}
	return out
}

// UpdateManager は現在のアップデート状態を保持し、定期チェックと手動チェック/適用を仲介する。
type UpdateManager struct {
	mu     sync.RWMutex
	status UpdateStatus
	prefs  *Preferences
	hub    *Hub
	kickCh chan struct{} // 手動チェック要求 (CheckNow から Run のループを起こす用、現状は直接実行するため主にRunの再評価トリガー)
	prefCh chan struct{} // preferences 変更通知
}

func newUpdateManager(prefs *Preferences, hub *Hub) *UpdateManager {
	return &UpdateManager{
		status: UpdateStatus{Current: version},
		prefs:  prefs,
		hub:    hub,
		kickCh: make(chan struct{}, 1),
		prefCh: make(chan struct{}, 1),
	}
}

func (m *UpdateManager) Status() UpdateStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *UpdateManager) setStatus(mutate func(*UpdateStatus)) UpdateStatus {
	m.mu.Lock()
	mutate(&m.status)
	s := m.status
	m.mu.Unlock()
	return s
}

// CheckNow は即時に最新リリースを取得し、状態を更新してから broadcast する。
func (m *UpdateManager) CheckNow(ctx context.Context) (UpdateStatus, error) {
	if os.Getenv("TMUXUI_AUTOUPDATE") == "0" {
		return UpdateStatus{Current: version, LastError: "auto-update disabled by TMUXUI_AUTOUPDATE=0"}, nil
	}
	latest, hasUpdate, err := checkForUpdate(ctx)
	now := time.Now()
	if err != nil {
		status := m.setStatus(func(s *UpdateStatus) {
			s.LastCheckedAt = now
			s.LastError = err.Error()
		})
		if m.hub != nil {
			m.hub.broadcastUpdateStatus(status)
		}
		return status, err
	}
	status := m.setStatus(func(s *UpdateStatus) {
		s.Current = version
		s.Latest = latest.Version()
		s.HasUpdate = hasUpdate
		s.LastCheckedAt = now
		s.LastError = ""
	})
	if m.hub != nil {
		m.hub.broadcastUpdateStatus(status)
	}
	return status, nil
}

// ApplyNow は差し替えを実行し、成功したら500ms後にrestartSelfするgoroutineを起動する。
func (m *UpdateManager) ApplyNow(ctx context.Context) (UpdateStatus, error) {
	if os.Getenv("TMUXUI_AUTOUPDATE") == "0" {
		return UpdateStatus{Current: version, LastError: "auto-update disabled by TMUXUI_AUTOUPDATE=0"}, nil
	}
	// apply後は/proc/self/exeがrename済みの.oldパスを返すため、apply前に元のパスを保存する
	exe, _ := selfupdate.ExecutablePath()
	latest, _, err := checkForUpdate(ctx)
	if err != nil {
		status := m.setStatus(func(s *UpdateStatus) {
			s.LastError = err.Error()
		})
		if m.hub != nil {
			m.hub.broadcastUpdateStatus(status)
		}
		return status, err
	}
	if err := applyUpdate(ctx, latest); err != nil {
		status := m.setStatus(func(s *UpdateStatus) {
			s.LastError = err.Error()
		})
		if m.hub != nil {
			m.hub.broadcastUpdateStatus(status)
		}
		return status, err
	}

	now := time.Now()
	status := m.setStatus(func(s *UpdateStatus) {
		s.Latest = latest.Version()
		s.HasUpdate = false
		s.Applied = true
		s.AppliedAt = now
		s.LastError = ""
	})
	if m.hub != nil {
		m.hub.broadcastUpdateStatus(status)
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := restartSelf(exe); err != nil {
			log.Printf("update: restart failed: %v", err)
		}
	}()

	return status, nil
}

// ApplyVersion は指定タグの Release にバイナリを差し替える (ダウングレード方向は非対応、
// selectableMinVersion 未満は弾く)。切替時は autoUpdate.autoApply を強制 OFF に更新し、
// 次回チェックで latest に自動昇格して指定版が上書きされないようにする。
func (m *UpdateManager) ApplyVersion(ctx context.Context, versionStr string) (UpdateStatus, error) {
	if os.Getenv("TMUXUI_AUTOUPDATE") == "0" {
		return UpdateStatus{Current: version, LastError: "auto-update disabled by TMUXUI_AUTOUPDATE=0"}, nil
	}
	if compareSemver(versionStr, selectableMinVersion) < 0 {
		err := fmt.Errorf("version %s is below the minimum selectable version %s", versionStr, selectableMinVersion)
		return UpdateStatus{Current: version, LastError: err.Error()}, err
	}

	// apply後は/proc/self/exeが.oldを指すためapply前に元パスを保存
	exe, _ := selfupdate.ExecutablePath()

	rel, err := detectVersion(ctx, versionStr)
	if err != nil {
		status := m.setStatus(func(s *UpdateStatus) { s.LastError = err.Error() })
		if m.hub != nil {
			m.hub.broadcastUpdateStatus(status)
		}
		return status, err
	}
	if err := applyUpdate(ctx, rel); err != nil {
		status := m.setStatus(func(s *UpdateStatus) { s.LastError = err.Error() })
		if m.hub != nil {
			m.hub.broadcastUpdateStatus(status)
		}
		return status, err
	}

	// 指定バージョンが auto-apply で上書きされないよう強制 OFF に
	if m.prefs != nil {
		cur, _ := m.prefs.GetAll()["autoUpdate"].(map[string]any)
		if cur == nil {
			cur = map[string]any{}
		}
		cur["autoApply"] = false
		m.prefs.Merge(map[string]any{"autoUpdate": cur})
		m.NotifyPreferenceChanged()
	}

	now := time.Now()
	status := m.setStatus(func(s *UpdateStatus) {
		s.Latest = rel.Version()
		s.HasUpdate = false
		s.Applied = true
		s.AppliedAt = now
		s.LastError = ""
	})
	if m.hub != nil {
		m.hub.broadcastUpdateStatus(status)
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		if err := restartSelf(exe); err != nil {
			log.Printf("update: restart failed: %v", err)
		}
	}()
	return status, nil
}

// NotifyPreferenceChanged は preferences 変更(間隔・enabled等)を Run のタイマーに反映させる。
func (m *UpdateManager) NotifyPreferenceChanged() {
	select {
	case m.prefCh <- struct{}{}:
	default:
	}
}

// Run は定期チェックループ。ctx.Done()/prefCh/kickCh/timer.C を待ち受ける。
// enabled=false の場合は次回発火をずっと先に(=実質停止)して待つ。
func (m *UpdateManager) Run(ctx context.Context) {
	if os.Getenv("TMUXUI_AUTOUPDATE") == "0" {
		log.Println("auto-update disabled by TMUXUI_AUTOUPDATE=0")
		return
	}
	p := loadAutoUpdatePrefs(m.prefs)
	timer := time.NewTimer(nextInterval(p))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.prefCh:
			p = loadAutoUpdatePrefs(m.prefs)
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(nextInterval(p))
		case <-m.kickCh:
			m.runCheckCycle(ctx, p)
		case <-timer.C:
			p = loadAutoUpdatePrefs(m.prefs)
			m.runCheckCycle(ctx, p)
			timer.Reset(nextInterval(p))
		}
	}
}

// runCheckCycle は1回分のチェック(+必要ならautoApply)を行う。ネットワークエラーはログとLastErrorに残すのみでプロセスは落とさない。
func (m *UpdateManager) runCheckCycle(ctx context.Context, p autoUpdatePrefs) {
	if !p.Enabled {
		return
	}
	status, err := m.CheckNow(ctx)
	if err != nil {
		log.Printf("update: check failed: %v", err)
		return
	}
	if p.AutoApply && status.HasUpdate {
		if _, err := m.ApplyNow(ctx); err != nil {
			log.Printf("update: auto apply failed: %v", err)
		}
	}
}

// nextInterval は enabled=false の場合実質停止するよう極めて長い間隔を返す。
func nextInterval(p autoUpdatePrefs) time.Duration {
	if !p.Enabled {
		return 365 * 24 * time.Hour
	}
	return time.Duration(p.IntervalHours) * time.Hour
}
