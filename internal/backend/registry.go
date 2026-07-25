package backend

import (
	"errors"
	"fmt"
	"strings"
)

// BackendRegistry は "<prefix>:<nativeID>" 形式の識別子を、対応するPaneBackendと
// ネイティブ識別子("main:0.%1"など、プレフィックスなし)に解決する。
// 登録済みプレフィックスに一致しない識別子は、旧クライアント/URLとの後方互換のため
// fallback(tmux)のネイティブ識別子として扱う。
type BackendRegistry struct {
	backends map[string]PaneBackend
	order    []string
	fallback string
}

func NewBackendRegistry(fallback string) *BackendRegistry {
	return &BackendRegistry{backends: map[string]PaneBackend{}, fallback: fallback}
}

func (r *BackendRegistry) Register(prefix string, b PaneBackend) {
	if _, exists := r.backends[prefix]; !exists {
		r.order = append(r.order, prefix)
	}
	r.backends[prefix] = b
}

// Resolve はidの先頭の"<prefix>:"部分が登録済みバックエンドと一致すればそれを使い、
// 一致しなければid全体をfallbackバックエンドのネイティブ識別子として扱う。
func (r *BackendRegistry) Resolve(id string) (backend PaneBackend, nativeID string, err error) {
	if id == "" {
		return nil, "", errors.New("registry: empty id")
	}
	if i := strings.IndexByte(id, ':'); i >= 0 {
		if b, ok := r.backends[id[:i]]; ok {
			native := id[i+1:]
			if !b.ValidTarget(native) {
				return nil, "", fmt.Errorf("registry: invalid target %q", id)
			}
			return b, native, nil
		}
	}
	b, ok := r.backends[r.fallback]
	if !ok {
		return nil, "", fmt.Errorf("registry: fallback backend %q not registered", r.fallback)
	}
	if !b.ValidTarget(id) {
		return nil, "", fmt.Errorf("registry: invalid target %q", id)
	}
	return b, id, nil
}

// ValidTarget はidがいずれかのバックエンドに解決できる有効な識別子かどうかを返す。
func (r *BackendRegistry) ValidTarget(id string) bool {
	_, _, err := r.Resolve(id)
	return err == nil
}

// ListSessions は登録済み全バックエンドのListSessions()をマージして返す。
// 各バックエンドのSyncSessionsをネイティブな(プレフィックスなしの)一覧で呼び出したうえで、
// 返却用にSession.Backendとpane.Targetへプレフィックスを付与する。
func (r *BackendRegistry) ListSessions() ([]Session, error) {
	var merged []Session
	var firstErr error
	for _, prefix := range r.order {
		b := r.backends[prefix]
		sessions, err := b.ListSessions()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		b.SyncSessions(sessions)
		for _, s := range sessions {
			merged = append(merged, prefixSession(prefix, s))
		}
	}
	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

func prefixSession(prefix string, s Session) Session {
	s.Backend = prefix
	windows := make([]Window, len(s.Windows))
	for wi, w := range s.Windows {
		panes := make([]Pane, len(w.Panes))
		for pi, p := range w.Panes {
			p.Target = prefix + ":" + p.Target
			panes[pi] = p
		}
		w.Panes = panes
		windows[wi] = w
	}
	s.Windows = windows
	return s
}
